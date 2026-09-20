package ddbstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"fengshen-desubber/billing_server/internal/cardkey"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	if os.Getenv("DDB_ENDPOINT") == "" {
		t.Skip("需要 DDB_ENDPOINT 指向 DynamoDB Local")
	}
	t.Setenv("TABLE_REDIMO", "fengshen-desubber")
	t.Setenv("CARD_PEPPER", "test-pepper")
	st, err := NewFromEnv(context.Background())
	if err != nil {
		t.Fatalf("NewFromEnv: %v", err)
	}
	return st
}

func TestAdminAndCardLifecycle(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	if err := st.EnsureAdmin(ctx, "adm_test_1"); err != nil {
		t.Fatalf("EnsureAdmin: %v", err)
	}
	u, err := st.GetUserByToken(ctx, "adm_test_1")
	if err != nil || !u.IsAdmin {
		t.Fatalf("GetUserByToken: %v %+v", err, u)
	}

	code, err := cardkey.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	c, err := st.CreateCard(ctx, "alice", code, 10, "batch-t1")
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	// 新卡默认 inactive 且未绑定机器
	if c.Balance != 10 || c.Status != CardInactive || c.MachineHash != "" {
		t.Fatalf("card: %+v", c)
	}
	// inactive 卡拒扣费
	if _, err := st.DebitCard(ctx, c, 1, "debit", "t0"); !errors.Is(err, ErrCardRevoked) {
		t.Fatalf("inactive debit: %v", err)
	}

	// 激活绑定机器码
	machine := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := st.BindCard(ctx, c, machine); err != nil {
		t.Fatalf("BindCard: %v", err)
	}
	if c.Status != CardActive || c.MachineHash != machine || c.BoundAt == 0 {
		t.Fatalf("after bind: %+v", c)
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

	// 充值
	nb, err := st.RechargeCard(ctx, c.ID, 5, "")
	if err != nil || nb != 15 {
		t.Fatalf("RechargeCard: %v %d", err, nb)
	}

	// 扣费
	got2, _ := st.GetCardByID(ctx, c.ID)
	nb, err = st.DebitCard(ctx, got2, 15, "debit", "task-x")
	if err != nil || nb != 0 {
		t.Fatalf("DebitCard: %v %d", err, nb)
	}
	// 余额不足
	got3, _ := st.GetCardByID(ctx, c.ID)
	if _, err := st.DebitCard(ctx, got3, 1, "debit", "task-y"); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("debit should be insufficient: %v", err)
	}

	// 吊销后拒扣
	if err := st.SetCardStatus(ctx, c.Hash, CardRevoked, "admin"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	got4, _ := st.GetCardByID(ctx, c.ID)
	if _, err := st.DebitCard(ctx, got4, 0, "debit", "t"); !errors.Is(err, ErrCardRevoked) {
		t.Fatalf("revoked debit: %v", err)
	}
	// 吊销卡拒解绑
	if err := st.UnbindCard(ctx, c.Hash, "admin"); !errors.Is(err, ErrCardRevoked) {
		t.Fatalf("revoked unbind: %v", err)
	}
	// 恢复落 inactive 且机器绑定已清
	if err := st.SetCardStatus(ctx, c.Hash, CardInactive, "admin"); err != nil {
		t.Fatalf("unrevoke: %v", err)
	}
	got5, _ := st.GetCardByID(ctx, c.ID)
	if got5.Status != CardInactive || got5.MachineHash != "" || got5.BoundAt != 0 {
		t.Fatalf("after unrevoke: %+v", got5)
	}
	// 重新绑定后可再解绑（幂等回 inactive）
	if err := st.BindCard(ctx, got5, machine); err != nil {
		t.Fatalf("re-bind: %v", err)
	}
	if err := st.UnbindCard(ctx, c.Hash, "admin"); err != nil {
		t.Fatalf("UnbindCard: %v", err)
	}
	got6, _ := st.GetCardByID(ctx, c.ID)
	if got6.Status != CardInactive || got6.MachineHash != "" {
		t.Fatalf("after unbind: %+v", got6)
	}

	// 流水：recharge + debit 各一条
	txs, err := st.ListTx(ctx, c.ID)
	if err != nil || len(txs) != 2 {
		t.Fatalf("ListTx: %v len=%d", err, len(txs))
	}
	if txs[0].Kind != "grant" || txs[1].Kind != "debit" {
		t.Fatalf("tx order: %+v", txs)
	}

	// 批次索引
	cards, err := st.ListCards(ctx, "batch-t1")
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
	code, _ := cardkey.Generate()
	c, err := st.CreateCard(ctx, "conc", code, 100, "")
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if err := st.BindCard(ctx, c, "machine-conc"); err != nil {
		t.Fatalf("BindCard: %v", err)
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
				cur, err := st.GetCardByID(ctx, c.ID)
				if err != nil {
					return
				}
				_, err = st.DebitCard(ctx, cur, 7, "debit", fmt.Sprintf("t-%d", i))
				if errors.Is(err, ErrInsufficientBalance) {
					// 并发冲突按不足返回：重读重试整流程
					cur2, _ := st.GetCardByID(ctx, c.ID)
					if cur2.Balance >= 7 {
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
	final, err := st.GetCardByID(ctx, c.ID)
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

func TestTaskQueueFlow(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	code, _ := cardkey.Generate()
	c, err := st.CreateCard(ctx, "worker", code, 10, "")
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if err := st.BindCard(ctx, c, "machine-worker"); err != nil {
		t.Fatalf("BindCard: %v", err)
	}

	t1 := &Task{ID: "task-" + code[:8] + "-1", CardID: c.ID, Provider: "las", SrcPath: "/tmp/a.mp4", DurationSec: 50, Cost: 1}
	if _, err := st.CreateTaskWithDebit(ctx, t1); err != nil {
		t.Fatalf("CreateTaskWithDebit: %v", err)
	}
	nb, _ := st.GetCardByID(ctx, c.ID)
	if nb.Balance != 9 {
		t.Fatalf("balance after debit: %d", nb.Balance)
	}

	got, err := st.NextQueued(ctx)
	if err != nil || got == nil || got.ID != t1.ID || got.Status != TaskProcessing {
		t.Fatalf("NextQueued: %v %+v", err, got)
	}
	if got.Provider != "las" {
		t.Fatalf("provider roundtrip: %+v", got)
	}
	// 队列已空
	empty, err := st.NextQueued(ctx)
	if err != nil || empty != nil {
		t.Fatalf("NextQueued empty: %v %+v", err, empty)
	}
	// ResetProcessing 排回
	if err := st.ResetProcessing(ctx); err != nil {
		t.Fatalf("ResetProcessing: %v", err)
	}
	got, err = st.NextQueued(ctx)
	if err != nil || got == nil || got.ID != t1.ID {
		t.Fatalf("NextQueued after reset: %v %+v", err, got)
	}
	// 完成
	if err := st.SetLasTaskID(ctx, t1.ID, "las-123"); err != nil {
		t.Fatalf("SetLasTaskID: %v", err)
	}
	if err := st.CompleteTask(ctx, t1.ID, "/tmp/out.mp4"); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
	done, _ := st.GetTask(ctx, t1.ID)
	if done.Status != TaskCompleted || done.ResultPath != "/tmp/out.mp4" || done.LasTaskID != "las-123" {
		t.Fatalf("completed: %+v", done)
	}

	// 失败退款幂等
	t2 := &Task{ID: "task-" + code[:8] + "-2", CardID: c.ID, SrcPath: "/tmp/b.mp4", DurationSec: 50, Cost: 2}
	if _, err := st.CreateTaskWithDebit(ctx, t2); err != nil {
		t.Fatalf("CreateTaskWithDebit t2: %v", err)
	}
	if err := st.FailTaskWithRefund(ctx, t2.ID, "boom"); err != nil {
		t.Fatalf("FailTaskWithRefund: %v", err)
	}
	if err := st.FailTaskWithRefund(ctx, t2.ID, "boom"); err != nil {
		t.Fatalf("FailTaskWithRefund idempotent: %v", err)
	}
	nb, _ = st.GetCardByID(ctx, c.ID)
	if nb.Balance != 9 { // 10 -1 -2 +2
		t.Fatalf("balance after refund: %d", nb.Balance)
	}
	ft, _ := st.GetTask(ctx, t2.ID)
	if ft.Status != TaskFailed || ft.Error != "boom" {
		t.Fatalf("failed task: %+v", ft)
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
