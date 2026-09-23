package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"fengshen-desubber/billing_server/internal/ddbstore"
	"fengshen-desubber/billing_server/internal/provider"
	"fengshen-desubber/billing_server/internal/testutil"
)

// testMachine 测试用机器码（64 位小写 hex）。
const testMachine = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// testMachine2 另一台机器（异机冲突用）。
const testMachine2 = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

const testRootPassword = "root-pass-123"

// fakeTOS 内存版对象存储:PUT/GET(Range)走 httptest,Delete 直接记录。
// 实现 provider.Uploader(预签名=直接拼测试服务 URL)。
type fakeTOS struct {
	srv     *httptest.Server
	mu      sync.Mutex
	objects map[string][]byte
	deleted []string
}

func newFakeTOS(t *testing.T) *fakeTOS {
	f := &fakeTOS{objects: map[string][]byte{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/o/")
		switch r.Method {
		case http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			f.mu.Lock()
			f.objects[key] = b
			f.mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			f.mu.Lock()
			b, ok := f.objects[key]
			f.mu.Unlock()
			if !ok {
				http.NotFound(w, r)
				return
			}
			http.ServeContent(w, r, key, time.Unix(0, 0), bytes.NewReader(b))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeTOS) PresignPut(ctx context.Context, key string, expires int64) (string, error) {
	return f.srv.URL + "/o/" + key, nil
}

func (f *fakeTOS) PresignGet(ctx context.Context, key string, expires int64) (string, error) {
	return f.srv.URL + "/o/" + key, nil
}

func (f *fakeTOS) Delete(ctx context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objects, key)
	f.deleted = append(f.deleted, key)
	return nil
}

func (f *fakeTOS) deletedKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deleted...)
}

type fakeOperator struct {
	fail        bool
	resultURL   string
	durationSec float64
}

func (f *fakeOperator) Submit(ctx context.Context, videoURL, clientToken string) (string, error) {
	return "las-task-1", nil
}

func (f *fakeOperator) Poll(ctx context.Context, taskID string) (string, string, string, float64, error) {
	if f.fail {
		return "FAILED", "", "subtitle too complex", 0, nil
	}
	return "COMPLETED", f.resultURL, "", f.durationSec, nil
}

type env struct {
	st       *ddbstore.Store
	srv      *httptest.Server
	tos      *fakeTOS
	op       *fakeOperator
	adminTok string // root 会话令牌
	userTok  string // 卡面（用户凭证）
	userID   int64  // 卡 id
	machine  string // 已激活卡的绑定机器码
}

func setup(t *testing.T, op provider.Operator) *env {
	t.Helper()
	testutil.FreshTable(t) // 独立表:共享表会跨轮残留任务/卡,互相污染
	st, err := ddbstore.NewFromEnv(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureRoot(context.Background(), testRootPassword); err != nil {
		t.Fatal(err)
	}
	// 平台注册表:fakeTOS 上传 + 注入算子,注册为 "las" 默认平台
	tos := newFakeTOS(t)
	fake, ok := op.(*fakeOperator)
	if op == nil {
		fake = &fakeOperator{}
		op = fake
	} else if !ok {
		fake = nil
	}
	reg := provider.NewRegistry("las")
	reg.Register(provider.Provider{Name: "las", Uploader: tos, Operator: op})

	e := &env{st: st, tos: tos, op: fake, machine: testMachine}
	e.srv = httptest.NewServer(New(st, reg, []byte("test-session-key")).Handler())
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

	code, b = e.doMachine(t, "POST", "/v1/cards/redeem", e.userTok, e.machine, nil)
	if code != 200 {
		t.Fatalf("activate: %d %s", code, b)
	}

	code, b = e.do(t, "POST", "/v1/admin/credits", e.adminTok,
		map[string]any{"user_id": e.userID, "amount": 5})
	if code != 200 {
		t.Fatalf("grant: %d %s", code, b)
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

// createTask 仅建单(不直传不提交):POST /v1/tasks 拿预签名 PUT URL。
func (e *env) createTask(t *testing.T) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest("POST", e.srv.URL+"/v1/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+e.userTok)
	req.Header.Set("X-Video-Filename", `C:\private\来源样片.mp4`)
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

// taskFlow 直传协议三步:建单→PUT 原片到"对象存储"→submit;返回 submit 的状态码/响应体。
func (e *env) taskFlow(t *testing.T, durationSec int64) (string, int, []byte) {
	t.Helper()
	if e.op != nil {
		e.op.durationSec = float64(durationSec)
	}
	code, b := e.createTask(t)
	if code != 201 {
		t.Fatalf("create: %d %s", code, b)
	}
	var created struct {
		TaskID    string                   `json:"task_id"`
		UploadURL string                   `json:"upload_url"`
		Account   machineAccountProjection `json:"account"`
	}
	json.Unmarshal(b, &created)
	if created.Account.MachineHash != e.machine || created.Account.Balance != 5 {
		t.Fatalf("create account projection missing: %+v", created.Account)
	}

	p := filepath.Join(t.TempDir(), "v.mp4")
	testutil.WriteTestMP4(t, p, durationSec)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPut, created.UploadURL, bytes.NewReader(data))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("tos put: %d", resp.StatusCode)
	}

	code, b = e.do(t, "POST", "/v1/tasks/"+created.TaskID+"/submit", e.userTok, nil)
	return created.TaskID, code, b
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
	resultSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(result)
	}))
	t.Cleanup(resultSrv.Close)
	e := setup(t, &fakeOperator{resultURL: resultSrv.URL + "/v.mp4"})

	taskID, code, b := e.taskFlow(t, 65) // 65s → 2 credits
	if code != 200 {
		t.Fatalf("submit: %d %s", code, b)
	}
	var sub struct {
		Cost    int64                    `json:"cost"`
		Account machineAccountProjection `json:"account"`
	}
	json.Unmarshal(b, &sub)
	if sub.Cost != 0 || sub.Account.Balance != 5 || sub.Account.MachineHash != e.machine {
		t.Fatalf("submit should defer settlement: %+v", sub)
	}

	completed := waitStatus(t, e, taskID, "completed")
	account := completed["account"].(map[string]any)
	if completed["cost"] != float64(2) || completed["duration_sec"] != float64(65) ||
		completed["original_filename"] != "来源样片.mp4" ||
		account["balance"] != float64(3) || account["machine_hash"] != e.machine {
		t.Fatalf("settlement wrong: %v", completed)
	}
	code, txBody := e.do(t, "GET", "/v1/admin/transactions?user_id="+fmt.Sprint(e.userID), e.adminTok, nil)
	if code != http.StatusOK {
		t.Fatalf("transactions: %d %s", code, txBody)
	}
	var txs []ddbstore.CreditTx
	json.Unmarshal(txBody, &txs)
	foundFilename := false
	for _, tx := range txs {
		if tx.TaskID == taskID && tx.OriginalFilename == "来源样片.mp4" {
			foundFilename = true
		}
	}
	if !foundFilename {
		t.Fatalf("transaction filename missing: %+v", txs)
	}

	// TOS 临时输入视频已即时清理
	if del := e.tos.deletedKeys(); len(del) != 1 || del[0] != "input/"+taskID+".mp4" {
		t.Fatalf("tos delete = %v", del)
	}

	// download 的 302 响应头同步携带机器账户，第二跳不带计费凭据。
	req, _ := http.NewRequest("GET", e.srv.URL+"/v1/tasks/"+taskID+"/download", nil)
	req.Header.Set("Authorization", "Bearer "+e.userTok)
	req.Header.Set("X-Machine-Hash", e.machine)
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusFound ||
		resp.Header.Get("X-Machine-Account-Hash") != e.machine ||
		resp.Header.Get("X-Machine-Account-Balance") != "3" {
		t.Fatalf("download redirect/account headers: %d %v", resp.StatusCode, resp.Header)
	}
	location := resp.Header.Get("Location")
	resp.Body.Close()
	resultResp, err := http.Get(location)
	if err != nil {
		t.Fatal(err)
	}
	defer resultResp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resultResp.Body)
	if resultResp.StatusCode != 200 || !bytes.Equal(buf.Bytes(), result) {
		t.Fatalf("download: %d len=%d", resultResp.StatusCode, buf.Len())
	}

	// 机器账户余额保持 3，无退款。
	_, b = e.do(t, "GET", "/v1/account", e.userTok, nil)
	var accountResp struct {
		Account machineAccountProjection `json:"account"`
	}
	json.Unmarshal(b, &accountResp)
	if accountResp.Account.Balance != 3 || accountResp.Account.MachineHash != e.machine {
		t.Fatalf("account want balance 3, got %+v", accountResp.Account)
	}
}

// TestAdminInternalTaskNoCredits 开发版内部通道:管理员会话建单(CardID=0),
// 全程不检查/不扣减余额,完成时 cost=0、不建机器账户,download 无账户头;
// 卡面用户看不到内部任务(404)。
func TestAdminInternalTaskNoCredits(t *testing.T) {
	result := []byte("fake-erase-result")
	resultSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(result)
	}))
	t.Cleanup(resultSrv.Close)
	e := setup(t, &fakeOperator{resultURL: resultSrv.URL + "/v.mp4", durationSec: 65})

	// 建单:管理员会话 + 机器码头(仅归因),不带卡。
	req, _ := http.NewRequest("POST", e.srv.URL+"/v1/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+e.adminTok)
	req.Header.Set("X-Video-Filename", `D:\dev\内部样片.mp4`)
	req.Header.Set("X-Machine-Hash", testMachine2)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("admin create: %d %s", resp.StatusCode, buf.Bytes())
	}
	var created struct {
		TaskID    string                    `json:"task_id"`
		UploadURL string                    `json:"upload_url"`
		Account   *machineAccountProjection `json:"account"`
	}
	json.Unmarshal(buf.Bytes(), &created)
	if created.Account != nil {
		t.Fatalf("internal task should not carry account projection: %+v", created.Account)
	}

	p := filepath.Join(t.TempDir(), "v.mp4")
	testutil.WriteTestMP4(t, p, 65)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	putReq, _ := http.NewRequest(http.MethodPut, created.UploadURL, bytes.NewReader(data))
	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	putResp.Body.Close()
	if putResp.StatusCode != 200 {
		t.Fatalf("tos put: %d", putResp.StatusCode)
	}

	// submit:管理员无机器账户也不应报 402。
	code, b := e.do(t, "POST", "/v1/tasks/"+created.TaskID+"/submit", e.adminTok, nil)
	if code != 200 {
		t.Fatalf("admin submit: %d %s", code, b)
	}

	// 轮询(管理员视角)到 completed:cost=0、无 account 投影。
	var completed map[string]any
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		code, b := e.do(t, "GET", "/v1/tasks/"+created.TaskID, e.adminTok, nil)
		if code == 200 {
			var st map[string]any
			json.Unmarshal(b, &st)
			if st["status"] == "completed" {
				completed = st
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if completed == nil {
		t.Fatalf("internal task never completed")
	}
	if completed["cost"] != float64(0) || completed["duration_sec"] != float64(65) ||
		completed["original_filename"] != "内部样片.mp4" {
		t.Fatalf("internal task settlement should be cost 0: %v", completed)
	}
	if account, ok := completed["account"]; ok && account != nil {
		t.Fatalf("internal task should not carry account: %v", account)
	}

	// 不建机器账户(自然也无流水)。
	if _, err := e.st.GetMachine(context.Background(), testMachine2); !errors.Is(err, ddbstore.ErrNotFound) {
		t.Fatalf("internal task should not create machine account: %v", err)
	}

	// 卡面用户看不到内部任务。
	code, _ = e.do(t, "GET", "/v1/tasks/"+created.TaskID, e.userTok, nil)
	if code != http.StatusNotFound {
		t.Fatalf("user token must not see internal task: %d", code)
	}

	// download:302 可用但无机器账户头。
	dlReq, _ := http.NewRequest("GET", e.srv.URL+"/v1/tasks/"+created.TaskID+"/download", nil)
	dlReq.Header.Set("Authorization", "Bearer "+e.adminTok)
	dlReq.Header.Set("X-Machine-Hash", testMachine2)
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	dlResp, err := client.Do(dlReq)
	if err != nil {
		t.Fatal(err)
	}
	defer dlResp.Body.Close()
	if dlResp.StatusCode != http.StatusFound ||
		dlResp.Header.Get("X-Machine-Account-Hash") != "" ||
		dlResp.Header.Get("X-Machine-Account-Balance") != "" {
		t.Fatalf("admin download: %d %v", dlResp.StatusCode, dlResp.Header)
	}
}

// 空注册表(Lambda 未配 TOS/LAS env):建单必须 503,否则产生无人处理的孤儿任务。
func TestCreateTaskRejectedWithoutProvider(t *testing.T) {
	testutil.FreshTable(t)
	st, err := ddbstore.NewFromEnv(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureRoot(context.Background(), testRootPassword); err != nil {
		t.Fatal(err)
	}
	e := &env{st: st, machine: testMachine}
	e.srv = httptest.NewServer(New(st, provider.NewRegistry(""), []byte("test-session-key")).Handler())
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
	code, b = e.doMachine(t, "POST", "/v1/cards/redeem", e.userTok, e.machine, nil)
	if code != 200 {
		t.Fatalf("activate: %d %s", code, b)
	}
	code, b = e.do(t, "POST", "/v1/admin/credits", e.adminTok,
		map[string]any{"user_id": e.userID, "amount": 5})
	if code != 200 {
		t.Fatalf("grant: %d %s", code, b)
	}

	code, b = e.createTask(t)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("empty registry should reject with 503, got %d %s", code, b)
	}
	_, b = e.do(t, "GET", "/v1/account", e.userTok, nil)
	var accountResp struct {
		Account machineAccountProjection `json:"account"`
	}
	json.Unmarshal(b, &accountResp)
	if accountResp.Account.Balance != 5 {
		t.Fatalf("no debit expected, got %d", accountResp.Account.Balance)
	}
}

func TestFailureRefunds(t *testing.T) {
	e := setup(t, &fakeOperator{fail: true})

	taskID, code, b := e.taskFlow(t, 30) // 1 credit
	if code != 200 {
		t.Fatalf("submit: %d %s", code, b)
	}

	st := waitStatus(t, e, taskID, "failed")
	if st["error"] == "" {
		t.Fatalf("failed task should carry error: %v", st)
	}

	// 失败路径同样清理 TOS 临时视频
	if del := e.tos.deletedKeys(); len(del) != 1 || del[0] != "input/"+taskID+".mp4" {
		t.Fatalf("tos delete = %v", del)
	}

	_, b = e.do(t, "GET", "/v1/account", e.userTok, nil)
	var accountResp struct {
		Account machineAccountProjection `json:"account"`
	}
	json.Unmarshal(b, &accountResp)
	if accountResp.Account.Balance != 5 {
		t.Fatalf("after refund want 5, got %d", accountResp.Account.Balance)
	}

	// 下载被拒
	code, _ = e.do(t, "GET", "/v1/tasks/"+taskID+"/download", e.userTok, nil)
	if code != http.StatusConflict {
		t.Fatalf("download of failed task want 409, got %d", code)
	}
}

func TestInsufficientBalance(t *testing.T) {
	// resultURL 必须非空:结算只在算子回报 COMPLETED+成片 URL+权威时长后触发,
	// 空 URL 视为结果未就绪保持 processing(该用例验证的是余额不足→结算失败置 failed)
	e := setup(t, &fakeOperator{resultURL: "http://unused/v.mp4"})
	taskID, code, b := e.taskFlow(t, 3600) // 60 credits > 5
	if code != http.StatusOK {
		t.Fatalf("submit should queue before settlement, got %d %s", code, b)
	}
	failed := waitStatus(t, e, taskID, "failed")
	if !strings.Contains(failed["error"].(string), "insufficient balance") {
		t.Fatalf("settlement failure missing: %v", failed)
	}
}

func TestAuthAndAdminGuard(t *testing.T) {
	e := setup(t, nil)

	code, _ := e.do(t, "GET", "/v1/account", "bad-token", nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", code)
	}
	code, _ = e.do(t, "POST", "/v1/admin/credits", e.userTok,
		map[string]any{"user_id": e.userID, "amount": 1})
	if code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", code)
	}
	// 他人任务不可见
	code, b := e.createTask(t)
	if code != 201 {
		t.Fatalf("create: %d %s", code, b)
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

// TestRedeemCardFlow 覆盖充值卡核销、同机幂等、异机拒绝及机器账户查询。
func TestRedeemCardFlow(t *testing.T) {
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
	code, b = e.doMachine(t, "GET", "/v1/account", card, testMachine, nil)
	if code != http.StatusForbidden || !bytes.Contains(b, []byte("card_not_activated")) {
		t.Fatalf("inactive balance: %d %s", code, b)
	}

	// 机器码格式校验：非 64 位小写 hex → 400
	code, b = e.doMachine(t, "POST", "/v1/cards/redeem", card, "XYZ", nil)
	if code != http.StatusBadRequest || !bytes.Contains(b, []byte("invalid_machine_hash")) {
		t.Fatalf("bad machine: %d %s", code, b)
	}

	// 激活成功：3 点转入机器账户，卡置 redeemed
	code, b = e.doMachine(t, "POST", "/v1/cards/redeem", card, testMachine, nil)
	if code != 200 {
		t.Fatalf("activate: %d %s", code, b)
	}
	var redeemed struct {
		Status  string                   `json:"status"`
		Account machineAccountProjection `json:"account"`
	}
	json.Unmarshal(b, &redeemed)
	if redeemed.Account.Balance != 8 || redeemed.Account.MachineHash != testMachine ||
		redeemed.Status != "redeemed" {
		t.Fatalf("redeem resp: %+v", redeemed)
	}

	// 同机幂等（不多入账）
	code, b = e.doMachine(t, "POST", "/v1/cards/redeem", card, testMachine, nil)
	if code != 200 {
		t.Fatalf("re-activate same machine: %d", code)
	}
	json.Unmarshal(b, &redeemed)
	if redeemed.Account.Balance != 8 {
		t.Fatalf("idempotent redeem double-credited: %+v", redeemed)
	}

	// 异机 → 409 card_bound_other
	code, b = e.doMachine(t, "POST", "/v1/cards/redeem", card, testMachine2, nil)
	if code != http.StatusConflict || !bytes.Contains(b, []byte("card_bound_other")) {
		t.Fatalf("activate other machine: %d %s", code, b)
	}

	// 业务接口机器码不符 → 403 machine_mismatch；不带机器码同罪
	code, b = e.doMachine(t, "GET", "/v1/account", card, testMachine2, nil)
	if code != http.StatusForbidden || !bytes.Contains(b, []byte("machine_mismatch")) {
		t.Fatalf("wrong machine balance: %d %s", code, b)
	}
	code, b = e.doMachine(t, "GET", "/v1/account", card, "", nil)
	if code != http.StatusForbidden || !bytes.Contains(b, []byte("machine_mismatch")) {
		t.Fatalf("no machine balance: %d %s", code, b)
	}

	// 绑定机正常
	code, b = e.doMachine(t, "GET", "/v1/account", card, testMachine, nil)
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
	code, b = e.doMachine(t, "POST", "/v1/cards/redeem", card, testMachine, nil)
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
	code, b = e.doMachine(t, "GET", "/v1/account", card, testMachine, nil)
	if code != 200 {
		t.Fatalf("balance after unrevoke: %d %s", code, b)
	}
	var accountResp struct {
		Account machineAccountProjection `json:"account"`
	}
	json.Unmarshal(b, &accountResp)
	if accountResp.Account.Balance != 8 {
		t.Fatalf("balance after unrevoke want 8, got %d", accountResp.Account.Balance)
	}
}

func TestGenerateCardsAutoBatch(t *testing.T) {
	e := setup(t, nil)
	generate := func(count int) []struct {
		Batch string `json:"batch"`
	} {
		code, body := e.do(t, "POST", "/v1/admin/cards/generate", e.adminTok,
			map[string]any{"count": count, "credits": 10, "name": "auto"})
		if code != http.StatusCreated {
			t.Fatalf("generate: %d %s", code, body)
		}
		var out struct {
			Cards []struct {
				Batch string `json:"batch"`
			} `json:"cards"`
		}
		json.Unmarshal(body, &out)
		return out.Cards
	}
	first := generate(2)
	if len(first) != 2 || first[0].Batch != "1" || first[1].Batch != "1" {
		t.Fatalf("first auto batch: %+v", first)
	}
	second := generate(1)
	if len(second) != 1 || second[0].Batch != "2" {
		t.Fatalf("second auto batch: %+v", second)
	}
}

// TestUnknownProvider X-Provider 未知名 → 400 unknown_provider；缺省落默认平台。
func TestUnknownProvider(t *testing.T) {
	e := setup(t, nil)

	req, _ := http.NewRequest("POST", e.srv.URL+"/v1/tasks", nil)
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
