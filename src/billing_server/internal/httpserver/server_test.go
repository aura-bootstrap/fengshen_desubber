package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fengshen-desubber/billing_server/internal/ddbstore"
	"fengshen-desubber/billing_server/internal/provider"
	"fengshen-desubber/billing_server/internal/testutil"
	"fengshen-desubber/billing_server/internal/worker"
)

// testMachine 测试用机器码（64 位小写 hex）。
const testMachine = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// testMachine2 另一台机器（异机冲突用）。
const testMachine2 = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

const testRootPassword = "root-pass-123"

type fakeUploader struct{}

func (fakeUploader) UploadAndPresign(ctx context.Context, localPath, key string, expires int64) (string, error) {
	return "https://tos.fake/" + key, nil
}

type fakeOperator struct {
	fail      bool
	resultBin []byte
}

func (f *fakeOperator) Submit(ctx context.Context, videoURL, clientToken string) (string, error) {
	return "las-task-1", nil
}

func (f *fakeOperator) Poll(ctx context.Context, taskID string) (string, string, string, error) {
	if f.fail {
		return "FAILED", "", "subtitle too complex", nil
	}
	return "COMPLETED", "https://result.fake/v.mp4", "", nil
}

func (f *fakeOperator) Download(ctx context.Context, url, dst string) error {
	return os.WriteFile(dst, f.resultBin, 0o644)
}

type env struct {
	st        *ddbstore.Store
	srv       *httptest.Server
	srcDir    string
	resultDir string
	adminTok  string // root 会话令牌
	userTok   string // 卡面（用户凭证）
	userID    int64  // 卡 id
	machine   string // 已激活卡的绑定机器码
}

func setup(t *testing.T, op provider.Operator) *env {
	t.Helper()
	dir := t.TempDir()
	testutil.FreshTable(t) // 独立表:共享表会跨轮残留队列任务,worker 会先消费陈旧任务
	st, err := ddbstore.NewFromEnv(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureRoot(context.Background(), testRootPassword); err != nil {
		t.Fatal(err)
	}
	// 平台注册表：fake 上传 + 注入算子，注册为 "las" 默认平台
	reg := provider.NewRegistry("las")
	reg.Register(provider.Provider{Name: "las", Uploader: fakeUploader{}, Operator: op})

	e := &env{st: st, srcDir: filepath.Join(dir, "src"), resultDir: filepath.Join(dir, "result"), machine: testMachine}
	os.MkdirAll(e.srcDir, 0o755)
	os.MkdirAll(e.resultDir, 0o755)
	e.srv = httptest.NewServer(New(st, e.srcDir, reg, []byte("test-session-key")).Handler())
	t.Cleanup(e.srv.Close)

	// root 登录拿会话令牌
	e.adminTok = e.login(t, "root", testRootPassword)

	// admin 建卡（0 点）→ 激活绑定机器码（核销 0 点、建机器账户）→ 给机器充 5 点
	code, b := e.do(t, "POST", "/v1/admin/users", e.adminTok,
		map[string]any{"name": "alice"})
	if code != 201 {
		t.Fatalf("create user: %d %s", code, b)
	}
	var u struct {
		UserID int64  `json:"user_id"`
		Token  string `json:"token"`
	}
	json.Unmarshal(b, &u)
	e.userID, e.userTok = u.UserID, u.Token

	code, b = e.doMachine(t, "POST", "/v1/activate", e.userTok, e.machine, nil)
	if code != 200 {
		t.Fatalf("activate: %d %s", code, b)
	}

	code, b = e.do(t, "POST", "/v1/admin/credits", e.adminTok,
		map[string]any{"user_id": e.userID, "amount": 5})
	if code != 200 {
		t.Fatalf("grant: %d %s", code, b)
	}

	// worker 后台跑
	if op != nil {
		w := worker.New(st, reg, e.resultDir)
		w.PollEvery = 10 * time.Millisecond
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		go w.Run(ctx)
	}
	return e
}

// login 调 /v1/admin/login 拿会话令牌。
func (e *env) login(t *testing.T, username, password string) string {
	t.Helper()
	code, b := e.doMachine(t, "POST", "/v1/admin/login", "", "", map[string]any{
		"username": username, "password": password,
	})
	if code != 200 {
		t.Fatalf("login %s: %d %s", username, code, b)
	}
	var out struct {
		Token string `json:"token"`
	}
	json.Unmarshal(b, &out)
	return out.Token
}

func (e *env) do(t *testing.T, method, path, token string, body any) (int, []byte) {
	t.Helper()
	return e.doMachine(t, method, path, token, e.machine, body)
}

// doMachine 显式指定机器码头发请求（空串 = 不带 X-Machine-Hash）。
func (e *env) doMachine(t *testing.T, method, path, token, machine string, body any) (int, []byte) {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rdr)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if machine != "" {
		req.Header.Set("X-Machine-Hash", machine)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.Bytes()
}

func (e *env) submitVideo(t *testing.T, durationSec int64) (int, []byte) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "v.mp4")
	testutil.WriteTestMP4(t, p, durationSec)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("POST", e.srv.URL+"/v1/tasks", bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+e.userTok)
	req.Header.Set("X-Video-Filename", "v.mp4")
	req.Header.Set("X-Machine-Hash", e.machine)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.Bytes()
}

func waitStatus(t *testing.T, e *env, taskID, want string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		code, b := e.do(t, "GET", "/v1/tasks/"+taskID, e.userTok, nil)
		if code == 200 {
			var st map[string]any
			json.Unmarshal(b, &st)
			if st["status"] == want {
				return st
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("task %s never reached %s", taskID, want)
	return nil
}

func TestFullFlowSuccess(t *testing.T) {
	result := []byte("fake-erase-result")
	e := setup(t, &fakeOperator{resultBin: result})

	code, b := e.submitVideo(t, 65) // 65s → 2 credits
	if code != 201 {
		t.Fatalf("submit: %d %s", code, b)
	}
	var created struct {
		TaskID  string `json:"task_id"`
		Cost    int64  `json:"cost"`
		Balance int64  `json:"balance"`
	}
	json.Unmarshal(b, &created)
	if created.Cost != 2 || created.Balance != 3 {
		t.Fatalf("cost/balance wrong: %+v", created)
	}

	waitStatus(t, e, created.TaskID, "completed")

	req, _ := http.NewRequest("GET", e.srv.URL+"/v1/tasks/"+created.TaskID+"/download", nil)
	req.Header.Set("Authorization", "Bearer "+e.userTok)
	req.Header.Set("X-Machine-Hash", e.machine)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	if resp.StatusCode != 200 || !bytes.Equal(buf.Bytes(), result) {
		t.Fatalf("download: %d len=%d", resp.StatusCode, buf.Len())
	}

	// 余额保持 3，无退款
	_, b = e.do(t, "GET", "/v1/balance", e.userTok, nil)
	var bal struct {
		Credits int64 `json:"credits"`
	}
	json.Unmarshal(b, &bal)
	if bal.Credits != 3 {
		t.Fatalf("balance want 3, got %d", bal.Credits)
	}
}

// 空注册表(Lambda 形态):建单必须 503 且不扣点,否则任务永远排队吞点。
func TestCreateTaskRejectedWithoutProvider(t *testing.T) {
	dir := t.TempDir()
	testutil.FreshTable(t)
	st, err := ddbstore.NewFromEnv(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureRoot(context.Background(), testRootPassword); err != nil {
		t.Fatal(err)
	}
	srcDir := filepath.Join(dir, "src")
	os.MkdirAll(srcDir, 0o755)
	e := &env{st: st, srcDir: srcDir, resultDir: filepath.Join(dir, "result"), machine: testMachine}
	e.srv = httptest.NewServer(New(st, srcDir, provider.NewRegistry(""), []byte("test-session-key")).Handler())
	t.Cleanup(e.srv.Close)

	e.adminTok = e.login(t, "root", testRootPassword)
	code, b := e.do(t, "POST", "/v1/admin/users", e.adminTok, map[string]any{"name": "alice"})
	if code != 201 {
		t.Fatalf("create user: %d %s", code, b)
	}
	var u struct {
		UserID int64  `json:"user_id"`
		Token  string `json:"token"`
	}
	json.Unmarshal(b, &u)
	e.userID, e.userTok = u.UserID, u.Token
	code, b = e.doMachine(t, "POST", "/v1/activate", e.userTok, e.machine, nil)
	if code != 200 {
		t.Fatalf("activate: %d %s", code, b)
	}
	code, b = e.do(t, "POST", "/v1/admin/credits", e.adminTok,
		map[string]any{"user_id": e.userID, "amount": 5})
	if code != 200 {
		t.Fatalf("grant: %d %s", code, b)
	}

	code, b = e.submitVideo(t, 30)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("empty registry should reject with 503, got %d %s", code, b)
	}
	_, b = e.do(t, "GET", "/v1/balance", e.userTok, nil)
	var bal struct {
		Credits int64 `json:"credits"`
	}
	json.Unmarshal(b, &bal)
	if bal.Credits != 5 {
		t.Fatalf("no debit expected, got %d", bal.Credits)
	}
}

func TestFailureRefunds(t *testing.T) {
	e := setup(t, &fakeOperator{fail: true})

	code, b := e.submitVideo(t, 30) // 1 credit
	if code != 201 {
		t.Fatalf("submit: %d %s", code, b)
	}
	var created struct {
		TaskID string `json:"task_id"`
	}
	json.Unmarshal(b, &created)

	st := waitStatus(t, e, created.TaskID, "failed")
	if st["error"] == "" {
		t.Fatalf("failed task should carry error: %v", st)
	}

	_, b = e.do(t, "GET", "/v1/balance", e.userTok, nil)
	var bal struct {
		Credits int64 `json:"credits"`
	}
	json.Unmarshal(b, &bal)
	if bal.Credits != 5 {
		t.Fatalf("after refund want 5, got %d", bal.Credits)
	}

	// 下载被拒
	code, _ = e.do(t, "GET", "/v1/tasks/"+created.TaskID+"/download", e.userTok, nil)
	if code != http.StatusConflict {
		t.Fatalf("download of failed task want 409, got %d", code)
	}
}

func TestInsufficientBalance(t *testing.T) {
	e := setup(t, nil)
	code, b := e.submitVideo(t, 3600) // 60 credits > 5
	if code != http.StatusPaymentRequired {
		t.Fatalf("want 402, got %d %s", code, b)
	}
}

func TestAuthAndAdminGuard(t *testing.T) {
	e := setup(t, nil)

	code, _ := e.do(t, "GET", "/v1/balance", "bad-token", nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", code)
	}
	code, _ = e.do(t, "POST", "/v1/admin/credits", e.userTok,
		map[string]any{"user_id": e.userID, "amount": 1})
	if code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", code)
	}
	// 他人任务不可见
	code, b := e.submitVideo(t, 10)
	if code != 201 {
		t.Fatalf("submit: %d %s", code, b)
	}
	var created struct {
		TaskID string `json:"task_id"`
	}
	json.Unmarshal(b, &created)

	code, _ = e.do(t, "GET", "/v1/tasks/"+created.TaskID, e.adminTok, nil)
	if code != 200 {
		t.Fatalf("admin should see task, got %d", code)
	}
}

// TestAdminSessionFlow 会话体系全链路：登录→me→建号→角色闸→禁用/重置/删除→登出吊销。
func TestAdminSessionFlow(t *testing.T) {
	e := setup(t, nil)

	// 错误密码 → 401（用一次性用户名，避免把 root@IP 打进连败锁）
	code, _ := e.doMachine(t, "POST", "/v1/admin/login", "", "", map[string]any{
		"username": "ghost-user", "password": "wrong-pass-1",
	})
	if code != http.StatusUnauthorized {
		t.Fatalf("bad login want 401, got %d", code)
	}

	// me：root 角色回显
	code, b := e.do(t, "GET", "/v1/admin/me", e.adminTok, nil)
	if code != 200 {
		t.Fatalf("me: %d %s", code, b)
	}
	var me struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	json.Unmarshal(b, &me)
	if me.Username != "root" || me.Role != "root" {
		t.Fatalf("me: %+v", me)
	}

	// root 建普通管理员
	code, b = e.do(t, "POST", "/v1/admin/accounts", e.adminTok, map[string]any{
		"username": "adm_ui1", "password": "admin-pass-1",
	})
	if code != 201 {
		t.Fatalf("create account: %d %s", code, b)
	}
	// 撞名 → 409
	code, _ = e.do(t, "POST", "/v1/admin/accounts", e.adminTok, map[string]any{
		"username": "adm_ui1", "password": "admin-pass-1",
	})
	if code != http.StatusConflict {
		t.Fatalf("dup account want 409, got %d", code)
	}
	// 非法用户名/密码 → 400
	code, _ = e.do(t, "POST", "/v1/admin/accounts", e.adminTok, map[string]any{
		"username": "ab", "password": "admin-pass-1",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("bad username want 400, got %d", code)
	}

	admTok := e.login(t, "adm_ui1", "admin-pass-1")

	// admin 不可见 accounts（root only）
	code, _ = e.do(t, "GET", "/v1/admin/accounts", admTok, nil)
	if code != http.StatusForbidden {
		t.Fatalf("admin list accounts want 403, got %d", code)
	}
	// admin 可做业务（发卡）
	code, b = e.do(t, "POST", "/v1/admin/cards/generate", admTok,
		map[string]any{"count": 1, "credits": 1, "batch": "adm-flow"})
	if code != 201 {
		t.Fatalf("admin generate cards: %d %s", code, b)
	}

	// root 列表含两个账号
	code, b = e.do(t, "GET", "/v1/admin/accounts", e.adminTok, nil)
	if code != 200 {
		t.Fatalf("root list accounts: %d %s", code, b)
	}
	var list struct {
		Accounts []ddbstore.Account `json:"accounts"`
	}
	json.Unmarshal(b, &list)
	if len(list.Accounts) != 2 {
		t.Fatalf("accounts: %+v", list.Accounts)
	}

	// root 账号不可经接口改动
	code, _ = e.do(t, "POST", "/v1/admin/accounts/root/status", e.adminTok, map[string]any{
		"status": "disabled", "version": 1,
	})
	if code != http.StatusBadRequest {
		t.Fatalf("mutate root want 400, got %d", code)
	}

	// 版本不符 → 409
	code, _ = e.do(t, "POST", "/v1/admin/accounts/adm_ui1/status", e.adminTok, map[string]any{
		"status": "disabled", "version": 99,
	})
	if code != http.StatusConflict {
		t.Fatalf("version mismatch want 409, got %d", code)
	}

	// 禁用（version 1 → 2）：在途会话即刻 403
	code, _ = e.do(t, "POST", "/v1/admin/accounts/adm_ui1/status", e.adminTok, map[string]any{
		"status": "disabled", "version": 1,
	})
	if code != 200 {
		t.Fatalf("disable: %d", code)
	}
	code, _ = e.do(t, "GET", "/v1/admin/me", admTok, nil)
	if code != http.StatusForbidden {
		t.Fatalf("disabled session want 403, got %d", code)
	}

	// 启用（version 2 → 3）
	code, _ = e.do(t, "POST", "/v1/admin/accounts/adm_ui1/status", e.adminTok, map[string]any{
		"status": "active", "version": 2,
	})
	if code != 200 {
		t.Fatalf("enable: %d", code)
	}

	// 重置密码（version 3 → 4）：旧密码失效
	code, _ = e.do(t, "POST", "/v1/admin/accounts/adm_ui1/password", e.adminTok, map[string]any{
		"password": "admin-pass-2", "version": 3,
	})
	if code != 200 {
		t.Fatalf("reset password: %d", code)
	}
	code, _ = e.doMachine(t, "POST", "/v1/admin/login", "", "", map[string]any{
		"username": "adm_ui1", "password": "admin-pass-1",
	})
	if code != http.StatusUnauthorized {
		t.Fatalf("old password after reset want 401, got %d", code)
	}
	admTok = e.login(t, "adm_ui1", "admin-pass-2")

	// 改密：本人验旧密改新密（version 4 → 5）
	code, _ = e.do(t, "POST", "/v1/admin/password", admTok, map[string]any{
		"old_password": "admin-pass-2", "new_password": "admin-pass-3",
	})
	if code != 200 {
		t.Fatalf("change password: %d", code)
	}
	code, _ = e.do(t, "GET", "/v1/admin/me", admTok, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("session after password change want 401, got %d", code)
	}

	// 删除（version 5）
	code, _ = e.do(t, "POST", "/v1/admin/accounts/adm_ui1/delete", e.adminTok, map[string]any{
		"version": 5,
	})
	if code != 200 {
		t.Fatalf("delete: %d", code)
	}
	code, _ = e.doMachine(t, "POST", "/v1/admin/login", "", "", map[string]any{
		"username": "adm_ui1", "password": "admin-pass-3",
	})
	if code != http.StatusUnauthorized {
		t.Fatalf("deleted account login want 401, got %d", code)
	}

	// 登出：bump 纪元，本令牌即刻失效
	code, _ = e.do(t, "POST", "/v1/admin/logout", e.adminTok, nil)
	if code != 200 {
		t.Fatalf("logout: %d", code)
	}
	code, _ = e.do(t, "GET", "/v1/admin/me", e.adminTok, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("me after logout want 401, got %d", code)
	}
}

// TestActivateFlow 激活=核销全链路：
// inactive 卡拒业务→激活（点数转机器账户）→同机幂等→异机 409→机器码不符 403
// →redeemed 卡拒解绑→吊销拒激活→恢复后绑定机可用。
func TestActivateFlow(t *testing.T) {
	e := setup(t, nil)

	// setup 已激活 userTok；另发一张未激活新卡
	code, b := e.do(t, "POST", "/v1/admin/cards/generate", e.adminTok,
		map[string]any{"count": 1, "credits": 3, "batch": "act-test"})
	if code != 201 {
		t.Fatalf("generate: %d %s", code, b)
	}
	var gen struct {
		Cards []struct {
			Code string `json:"code"`
		} `json:"cards"`
	}
	json.Unmarshal(b, &gen)
	card := gen.Cards[0].Code

	// 未激活：带不带机器码都 403 card_not_activated
	code, b = e.doMachine(t, "GET", "/v1/balance", card, testMachine, nil)
	if code != http.StatusForbidden || !bytes.Contains(b, []byte("card_not_activated")) {
		t.Fatalf("inactive balance: %d %s", code, b)
	}

	// 机器码格式校验：非 64 位小写 hex → 400
	code, b = e.doMachine(t, "POST", "/v1/activate", card, "XYZ", nil)
	if code != http.StatusBadRequest || !bytes.Contains(b, []byte("invalid_machine_hash")) {
		t.Fatalf("bad machine: %d %s", code, b)
	}

	// 激活成功：3 点转入机器账户，卡置 redeemed
	code, b = e.doMachine(t, "POST", "/v1/activate", card, testMachine, nil)
	if code != 200 {
		t.Fatalf("activate: %d %s", code, b)
	}
	var act struct {
		Credits     int64  `json:"credits"`
		MachineHash string `json:"machine_hash"`
		Status      string `json:"status"`
	}
	json.Unmarshal(b, &act)
	// credits 为机器累计余额:setup 已给同机充 5,本卡核销 3 → 8
	if act.Credits != 8 || act.MachineHash != testMachine || act.Status != "redeemed" {
		t.Fatalf("activate resp: %+v", act)
	}

	// 同机幂等（不多入账）
	code, b = e.doMachine(t, "POST", "/v1/activate", card, testMachine, nil)
	if code != 200 {
		t.Fatalf("re-activate same machine: %d", code)
	}
	json.Unmarshal(b, &act)
	if act.Credits != 8 {
		t.Fatalf("idempotent activate double-credited: %+v", act)
	}

	// 异机 → 409 card_bound_other
	code, b = e.doMachine(t, "POST", "/v1/activate", card, testMachine2, nil)
	if code != http.StatusConflict || !bytes.Contains(b, []byte("card_bound_other")) {
		t.Fatalf("activate other machine: %d %s", code, b)
	}

	// 业务接口机器码不符 → 403 machine_mismatch；不带机器码同罪
	code, b = e.doMachine(t, "GET", "/v1/balance", card, testMachine2, nil)
	if code != http.StatusForbidden || !bytes.Contains(b, []byte("machine_mismatch")) {
		t.Fatalf("wrong machine balance: %d %s", code, b)
	}
	code, b = e.doMachine(t, "GET", "/v1/balance", card, "", nil)
	if code != http.StatusForbidden || !bytes.Contains(b, []byte("machine_mismatch")) {
		t.Fatalf("no machine balance: %d %s", code, b)
	}

	// 绑定机正常
	code, b = e.doMachine(t, "GET", "/v1/balance", card, testMachine, nil)
	if code != 200 {
		t.Fatalf("bound machine balance: %d %s", code, b)
	}

	// redeemed 卡拒解绑（点数已转机器账户，解绑不能复活卡面）→ 409
	code, b = e.do(t, "POST", "/v1/admin/cards/unbind", e.adminTok,
		map[string]any{"code": card})
	if code != http.StatusConflict || !bytes.Contains(b, []byte("card_redeemed")) {
		t.Fatalf("redeemed unbind want 409, got %d %s", code, b)
	}

	// 吊销卡：激活 403 card_revoked；解绑 400
	code, _ = e.do(t, "POST", "/v1/admin/cards/revoke", e.adminTok,
		map[string]any{"card": card})
	if code != 200 {
		t.Fatalf("revoke: %d", code)
	}
	code, b = e.doMachine(t, "POST", "/v1/activate", card, testMachine, nil)
	if code != http.StatusForbidden || !bytes.Contains(b, []byte("card_revoked")) {
		t.Fatalf("revoked activate: %d %s", code, b)
	}
	code, _ = e.do(t, "POST", "/v1/admin/cards/unbind", e.adminTok,
		map[string]any{"code": card})
	if code != http.StatusBadRequest {
		t.Fatalf("revoked unbind want 400, got %d", code)
	}

	// 恢复：已核销卡回 redeemed（机器绑定保留），绑定机余额仍 8
	code, _ = e.do(t, "POST", "/v1/admin/cards/unrevoke", e.adminTok,
		map[string]any{"card": card})
	if code != 200 {
		t.Fatalf("unrevoke: %d", code)
	}
	code, b = e.doMachine(t, "GET", "/v1/balance", card, testMachine, nil)
	if code != 200 {
		t.Fatalf("balance after unrevoke: %d %s", code, b)
	}
	var bal struct {
		Credits int64 `json:"credits"`
	}
	json.Unmarshal(b, &bal)
	if bal.Credits != 8 {
		t.Fatalf("balance after unrevoke want 8, got %d", bal.Credits)
	}
}

// TestUnknownProvider X-Provider 未知名 → 400 unknown_provider；缺省落默认平台。
func TestUnknownProvider(t *testing.T) {
	e := setup(t, nil)
	p := filepath.Join(t.TempDir(), "v.mp4")
	testutil.WriteTestMP4(t, p, 10)
	data, _ := os.ReadFile(p)

	req, _ := http.NewRequest("POST", e.srv.URL+"/v1/tasks", bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+e.userTok)
	req.Header.Set("X-Video-Filename", "v.mp4")
	req.Header.Set("X-Machine-Hash", e.machine)
	req.Header.Set("X-Provider", "no-such")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	if resp.StatusCode != http.StatusBadRequest || !bytes.Contains(buf.Bytes(), []byte("unknown_provider")) {
		t.Fatalf("unknown provider: %d %s", resp.StatusCode, buf.Bytes())
	}
}
