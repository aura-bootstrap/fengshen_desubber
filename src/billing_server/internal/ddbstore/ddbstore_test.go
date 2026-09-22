package ddbstore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"fengshen-desubber/billing_server/internal/cardkey"
	"fengshen-desubber/billing_server/internal/testutil"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	testutil.FreshTable(t) // 独立表:共享表会跨轮残留机器余额/队列/审计
	st, err := NewFromEnv(context.Background())
	if err != nil {
		t.Fatalf("NewFromEnv: %v", err)
	}
	return st
}

func TestAccountLifecycle(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	// EnsureRoot 幂等种入 + 登录校验
	if err := st.EnsureRoot(ctx, "root-pass-123"); err != nil {
		t.Fatalf("EnsureRoot: %v", err)
	}
	if err := st.EnsureRoot(ctx, "root-pass-456"); err != nil {
		t.Fatalf("EnsureRoot idempotent: %v", err)
	}
	root, ok, err := st.CheckLogin(ctx, RoleRoot, "root-pass-123")
	if err != nil || !ok || root.Role != RoleRoot {
		t.Fatalf("CheckLogin root: %v %v %+v", err, ok, root)
	}
	// 第二次 EnsureRoot 未覆盖密码
	if _, ok, _ := st.CheckLogin(ctx, RoleRoot, "root-pass-456"); ok {
		t.Fatal("EnsureRoot overwrote password")
	}
	// 错误密码/不存在账号统一 false
	if _, ok, _ := st.CheckLogin(ctx, RoleRoot, "wrong"); ok {
		t.Fatal("wrong password accepted")
	}
	if _, ok, _ := st.CheckLogin(ctx, "no-such-user", "x"); ok {
		t.Fatal("ghost user accepted")
	}

	// 建号撞名 + 校验规则
	hash, _ := HashPasswordNew("admin-pass-1")
	epoch, _ := NewPassEpoch()
	now := st.Now().Unix()
	a := &Account{Username: "adm_t1", Role: RoleAdmin, PassHash: hash, Status: UserActive,
		PassEpoch: epoch, Version: 1, CreatedAt: now, CreatedBy: "root", UpdatedAt: now}
	ok, err = st.CreateAccount(ctx, a)
	if err != nil || !ok {
		t.Fatalf("CreateAccount: %v %v", err, ok)
	}
	ok, err = st.CreateAccount(ctx, a)
	if err != nil || ok {
		t.Fatalf("CreateAccount dup: %v %v", err, ok)
	}
	if msg := ValidateUsername("root"); msg == "" || ValidateUsername("ab") == "" ||
		ValidateUsername("bad name") == "" || ValidateUsername("good_name-1.x") != "" {
		t.Fatal("ValidateUsername rules broken")
	}
	if ValidatePassword("short") == "" || ValidatePassword("12345678") != "" {
		t.Fatal("ValidatePassword rules broken")
	}

	// 改密 CAS：版本/纪元推进，旧密码失效
	got, err := st.GetAccount(ctx, "adm_t1")
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	newHash, _ := HashPasswordNew("admin-pass-2")
	ok, err = st.CASAccount(ctx, got, func(n *Account) {
		n.PassHash = newHash
		n.PassEpoch++
		n.Version++
	})
	if err != nil || !ok {
		t.Fatalf("CASAccount: %v %v", err, ok)
	}
	if _, ok, _ := st.CheckLogin(ctx, "adm_t1", "admin-pass-1"); ok {
		t.Fatal("old password still valid")
	}
	if _, ok, _ := st.CheckLogin(ctx, "adm_t1", "admin-pass-2"); !ok {
		t.Fatal("new password rejected")
	}

	// 列表含 root 与 adm_t1，PassHash 序列化不出现在 Account JSON 之外
	list, err := st.ListAccounts(ctx)
	if err != nil || len(list) < 2 {
		t.Fatalf("ListAccounts: %v len=%d", err, len(list))
	}

	// 删除后查不到
	got, _ = st.GetAccount(ctx, "adm_t1")
	ok, err = st.DeleteAccount(ctx, got)
	if err != nil || !ok {
		t.Fatalf("DeleteAccount: %v %v", err, ok)
	}
	if _, err := st.GetAccount(ctx, "adm_t1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}

func TestCardAndMachineLifecycle(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	code, err := cardkey.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	c, err := st.CreateCard(ctx, "alice", code, 10, "batch-m1")
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	// 新卡默认 inactive 且未绑定机器
	if c.Balance != 10 || c.Status != CardInactive || c.MachineHash != "" || c.Credited {
		t.Fatalf("card: %+v", c)
	}

	// 归一化：小写+无横线也能命中（v2 哈希）
	got, err := st.GetCardByCode(ctx, cardkey.Normalize(code))
	if err != nil || got.ID != c.ID {
		t.Fatalf("GetCardByCode normalized: %v", err)
	}
	// v1 裸哈希查不到新卡
	if _, err := st.GetCardByHash(ctx, cardkey.HashV1(code)); !errors.Is(err, ErrCardNotFound) {
		t.Fatalf("v1 hash should miss: %v", err)
	}
	// 按 id 查
	byID, err := st.GetCardByID(ctx, c.ID)
	if err != nil || byID.Hash != c.Hash {
		t.Fatalf("GetCardByID: %v", err)
	}

	// 核销：卡面 10 点转入机器账户，卡置 redeemed
	machine := fmt.Sprintf("mach-%d", time.Now().UnixNano())
	bal, err := st.EnsureRedeemed(ctx, c, machine)
	if err != nil || bal != 10 {
		t.Fatalf("EnsureRedeemed: %v %d", err, bal)
	}
	if c.Status != CardRedeemed || !c.Credited || c.Balance != 0 || c.MachineHash != machine || c.BoundAt == 0 {
		t.Fatalf("after redeem: %+v", c)
	}
	// 幂等：再核销不多入账
	bal, err = st.EnsureRedeemed(ctx, c, machine)
	if err != nil || bal != 10 {
		t.Fatalf("EnsureRedeemed idempotent: %v %d", err, bal)
	}

	// redeemed 卡拒解绑
	if err := st.UnbindCard(ctx, c.Hash, "admin"); !errors.Is(err, ErrCardRedeemed) {
		t.Fatalf("redeemed unbind: %v", err)
	}

	// 机器账户扣费/入账：超额拒扣（返回当前余额+ErrInsufficientBalance）
	nb, err := st.DebitMachine(ctx, machine, 15, "debit", "task-x", c.ID)
	if !errors.Is(err, ErrInsufficientBalance) || nb != 10 {
		t.Fatalf("debit insufficient: %v %d", err, nb)
	}
	nb, err = st.DebitMachine(ctx, machine, 10, "debit", "task-x", c.ID)
	if err != nil || nb != 0 {
		t.Fatalf("DebitMachine: %v %d", err, nb)
	}
	nb, err = st.CreditMachine(ctx, machine, 5, "grant", "by-admin", c.ID)
	if err != nil || nb != 5 {
		t.Fatalf("CreditMachine: %v %d", err, nb)
	}

	// 吊销：业务读取态由服务端判；恢复已核销卡回 redeemed
	if err := st.SetCardStatus(ctx, c.Hash, CardRevoked, "admin"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := st.UnbindCard(ctx, c.Hash, "admin"); !errors.Is(err, ErrCardRevoked) {
		t.Fatalf("revoked unbind: %v", err)
	}
	if err := st.SetCardStatus(ctx, c.Hash, CardInactive, "admin"); err != nil {
		t.Fatalf("unrevoke: %v", err)
	}
	got2, _ := st.GetCardByID(ctx, c.ID)
	if got2.Status != CardRedeemed || got2.MachineHash != machine {
		t.Fatalf("after unrevoke credited card: %+v", got2)
	}

	// 机器流水：redeem grant + debit + admin grant = 3 条
	txs, err := st.ListMachineTx(ctx, machine)
	if err != nil || len(txs) != 3 {
		t.Fatalf("ListMachineTx: %v len=%d", err, len(txs))
	}
	if txs[0].Kind != "grant" || txs[1].Kind != "debit" || txs[2].Kind != "grant" {
		t.Fatalf("tx kinds: %+v", txs)
	}
	if txs[2].BalanceAfter != 5 {
		t.Fatalf("tx balance: %+v", txs[2])
	}

	// 批次索引
	cards, err := st.ListCards(ctx, "batch-m1")
	if err != nil || len(cards) == 0 {
		t.Fatalf("ListCards batch: %v len=%d", err, len(cards))
	}

	// 审计链非空且有序
	audit, err := st.AuditList(ctx)
	if err != nil || len(audit) < 3 {
		t.Fatalf("AuditList: %v len=%d", err, len(audit))
	}
	for i := 1; i < len(audit); i++ {
		if audit[i-1]["sk"].(string) >= audit[i]["sk"].(string) {
			t.Fatalf("audit sk not ordered: %v >= %v", audit[i-1]["sk"], audit[i]["sk"])
		}
	}
}

func TestConcurrentDebitConsistency(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	machine := fmt.Sprintf("mach-conc-%d", time.Now().UnixNano())
	if _, err := st.CreditMachine(ctx, machine, 100, "grant", "seed", 0); err != nil {
		t.Fatalf("seed: %v", err)
	}
	const n = 10
	var wg sync.WaitGroup
	var okCount int64
	var mu sync.Mutex
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for {
				_, err := st.DebitMachine(ctx, machine, 7, "debit", fmt.Sprintf("t-%d", i), 0)
				if errors.Is(err, ErrInsufficientBalance) {
					// 并发冲突按不足返回：重读重试整流程
					m, _ := st.GetMachine(ctx, machine)
					if m.Balance >= 7 {
						continue
					}
					return
				}
				if err == nil {
					mu.Lock()
					okCount++
					mu.Unlock()
				}
				return
			}
		}(i)
	}
	wg.Wait()
	final, err := st.GetMachine(ctx, machine)
	if err != nil {
		t.Fatalf("final: %v", err)
	}
	if final.Balance != 100-okCount*7 {
		t.Fatalf("balance drift: got %d, okCount=%d", final.Balance, okCount)
	}
	if final.Balance < 0 {
		t.Fatalf("negative balance: %d", final.Balance)
	}
}

func TestTaskLifecycle(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	code, _ := cardkey.Generate()
	c, err := st.CreateCard(ctx, "worker", code, 10, "")
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	machine := fmt.Sprintf("mach-task-%d", time.Now().UnixNano())
	if _, err := st.EnsureRedeemed(ctx, c, machine); err != nil {
		t.Fatalf("EnsureRedeemed: %v", err)
	}

	// 建单不扣点:uploading 态,余额不动
	t1 := &Task{
		ID: "task-" + code[:8] + "-1", CardID: c.ID, Provider: "las", SrcKey: "input/a.mp4",
		SourceName: "样片.mp4", SourcePath: `D:\视频\样片.mp4`,
	}
	if err := st.CreateUploadingTask(ctx, t1); err != nil {
		t.Fatalf("CreateUploadingTask: %v", err)
	}
	if t1.Status != TaskUploading || t1.MachineHash != machine {
		t.Fatalf("uploading task: %+v", t1)
	}
	m, _ := st.GetMachine(ctx, machine)
	if m.Balance != 10 {
		t.Fatalf("balance before submit: %d", m.Balance)
	}

	// submit 扣点置 processing;重复 submit 拒
	if _, err := st.SubmitTaskWithDebit(ctx, t1.ID, 50, 1); err != nil {
		t.Fatalf("SubmitTaskWithDebit: %v", err)
	}
	m, _ = st.GetMachine(ctx, machine)
	if m.Balance != 9 {
		t.Fatalf("balance after debit: %d", m.Balance)
	}
	if _, err := st.SubmitTaskWithDebit(ctx, t1.ID, 50, 1); !errors.Is(err, ErrTaskState) {
		t.Fatalf("re-submit should be ErrTaskState: %v", err)
	}
	got, _ := st.GetTask(ctx, t1.ID)
	if got.Status != TaskProcessing || got.Provider != "las" || got.DurationSec != 50 || got.Cost != 1 ||
		got.SubmittedAt == 0 || got.SourceName != "样片.mp4" || got.SourcePath != `D:\视频\样片.mp4` {
		t.Fatalf("processing task: %+v", got)
	}

	// 完成:记录算子侧成片 URL
	if err := st.SetLasTaskID(ctx, t1.ID, "las-123"); err != nil {
		t.Fatalf("SetLasTaskID: %v", err)
	}
	if err := st.CompleteTask(ctx, t1.ID, "https://tos/out.mp4"); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
	done, _ := st.GetTask(ctx, t1.ID)
	if done.Status != TaskCompleted || done.ResultURL != "https://tos/out.mp4" || done.LasTaskID != "las-123" ||
		done.FinishedAt == 0 || done.FinishedAt < done.SubmittedAt {
		t.Fatalf("completed: %+v", done)
	}

	// processing 失败退款且幂等
	t2 := &Task{ID: "task-" + code[:8] + "-2", CardID: c.ID, SrcKey: "input/b.mp4"}
	if err := st.CreateUploadingTask(ctx, t2); err != nil {
		t.Fatalf("CreateUploadingTask t2: %v", err)
	}
	if _, err := st.SubmitTaskWithDebit(ctx, t2.ID, 50, 2); err != nil {
		t.Fatalf("SubmitTaskWithDebit t2: %v", err)
	}
	if err := st.FailTaskWithRefund(ctx, t2.ID, "boom"); err != nil {
		t.Fatalf("FailTaskWithRefund: %v", err)
	}
	if err := st.FailTaskWithRefund(ctx, t2.ID, "boom"); err != nil {
		t.Fatalf("FailTaskWithRefund idempotent: %v", err)
	}
	m, _ = st.GetMachine(ctx, machine)
	if m.Balance != 9 { // 10 -1 -2 +2
		t.Fatalf("balance after refund: %d", m.Balance)
	}
	ft, _ := st.GetTask(ctx, t2.ID)
	if ft.Status != TaskFailed || ft.Error != "boom" || ft.FinishedAt == 0 {
		t.Fatalf("failed task: %+v", ft)
	}

	audit, err := st.AuditList(ctx)
	if err != nil {
		t.Fatalf("AuditList tasks: %v", err)
	}
	actions := map[string]int{}
	for _, entry := range audit {
		if entry["scope"] != "task" {
			continue
		}
		action, _ := entry["action"].(string)
		actions[action]++
		if entry["task_id"] == t1.ID && action == "task_debited" {
			if entry["source_path"] != `D:\视频\样片.mp4` || entry["duration_sec"] != float64(50) ||
				entry["cost"] != float64(1) || entry["machine_hash"] != machine || entry["balance_after"] != float64(9) {
				t.Fatalf("debit audit incomplete: %#v", entry)
			}
		}
	}
	if actions["task_created"] != 2 || actions["task_debited"] != 2 ||
		actions["task_completed"] != 1 || actions["task_failed"] != 1 {
		t.Fatalf("task audit actions: %#v", actions)
	}
}

func TestRateLimitAndFailLock(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	id := fmt.Sprintf("rl-%d", time.Now().UnixNano())
	n, err := st.HitRate(ctx, "status", id, 60)
	if err != nil || n != 1 {
		t.Fatalf("HitRate: %v %d", err, n)
	}
	n, _ = st.HitRate(ctx, "status", id, 60)
	if n != 2 {
		t.Fatalf("HitRate second: %d", n)
	}
	for i := int64(0); i < 9; i++ {
		locked, err := st.FailLock(ctx, "auth", id, 10, 1800)
		if err != nil || locked != 0 {
			t.Fatalf("FailLock %d: %v %d", i, err, locked)
		}
	}
	locked, err := st.FailLock(ctx, "auth", id, 10, 1800)
	if err != nil || locked != 1800 {
		t.Fatalf("FailLock trip: %v %d", err, locked)
	}
	rem, err := st.LockedFor(ctx, "auth", id)
	if err != nil || rem <= 0 {
		t.Fatalf("LockedFor: %v %d", err, rem)
	}
	if err := st.ResetFail(ctx, "auth", id); err != nil {
		t.Fatalf("ResetFail: %v", err)
	}
	if rem, _ := st.LockedFor(ctx, "auth", id); rem != 0 {
		t.Fatalf("LockedFor after reset: %d", rem)
	}
}
