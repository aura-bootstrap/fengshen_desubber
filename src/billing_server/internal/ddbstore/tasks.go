package ddbstore

import (
	"context"
	"encoding/json"
	"errors"
	"log"

	redimo "github.com/aura-studio/redimo/v2"
)

const (
	TaskUploading  = "uploading" // 已建单待发 TOS 直传+submit,未扣点
	TaskProcessing = "processing"
	TaskCompleted  = "completed"
	TaskFailed     = "failed"
)

// Task 去字幕任务（落库 JSON 字段序固定=SETCAS 基线前提）。
// Provider 为处理平台名（provider.Registry 注册名），空 = 默认平台。
// CardID 为提交任务所用凭证卡（归因/可见性）；费用走 MachineHash 机器账户。
// SrcKey 为 TOS 输入对象键（客户端直传）；ResultURL 为算子侧成片地址（服务端不中转）。
type Task struct {
	ID            string `json:"id"`
	CardID        int64  `json:"card_id"`
	CardHash      string `json:"card_hash"`
	MachineHash   string `json:"machine_hash"`
	Provider      string `json:"provider"`
	SrcKey        string `json:"src_key"`
	ResultURL     string `json:"result_url,omitempty"`
	DurationSec   int64  `json:"duration_sec"`
	Cost          int64  `json:"cost"`
	Status        string `json:"status"`
	Error         string `json:"error,omitempty"`
	LasTaskID     string `json:"las_task_id,omitempty"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
	CardMasked    string `json:"card_masked,omitempty"`
	SourceName    string `json:"source_name,omitempty"`
	SourcePath    string `json:"source_path,omitempty"`
	SubmittedAt   int64  `json:"submitted_at,omitempty"`
	FinishedAt    int64  `json:"finished_at,omitempty"`
	EstimatedCost int64  `json:"estimated_cost,omitempty"`
	Charged       bool   `json:"charged,omitempty"`
	BalanceAfter  *int64 `json:"balance_after,omitempty"`
}

func (s *Store) GetTask(ctx context.Context, id string) (*Task, error) {
	rv, err := s.cli.WithContext(ctx).GET(keyTask + id)
	if err != nil {
		return nil, err
	}
	if rv.Empty() {
		return nil, ErrNotFound
	}
	var t Task
	if err := json.Unmarshal([]byte(rv.String()), &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// casTask 整值 CAS 改写任务，有限重试。
func (s *Store) casTask(ctx context.Context, t *Task, mutate func(*Task)) (bool, error) {
	for attempt := 0; attempt < 5; attempt++ {
		oldJSON := mustMarshal(t)
		next := *t
		next.UpdatedAt = s.Now().Unix()
		mutate(&next)
		ok, err := s.cli.WithContext(ctx).SETCAS(keyTask+t.ID,
			redimo.StringValue{S: mustMarshal(&next)}, redimo.StringValue{S: oldJSON}, true)
		if err != nil {
			return false, err
		}
		if ok {
			*t = next
			return true, nil
		}
		fresh, err := s.GetTask(ctx, t.ID)
		if err != nil {
			return false, err
		}
		*t = *fresh
	}
	return false, nil
}

func taskAuditEntry(t *Task, action string, ok bool) AuditEntry {
	estimatedCost := t.EstimatedCost
	if estimatedCost == 0 {
		estimatedCost = t.Cost
	}
	return AuditEntry{
		TS: t.CreatedAt, Actor: "system", Action: action, Target: t.ID, OK: ok,
		TaskID: t.ID, MachineHash: t.MachineHash, CardID: t.CardID, CardMasked: t.CardMasked,
		Provider: t.Provider, SourceName: t.SourceName, SourcePath: t.SourcePath,
		DurationSec: t.DurationSec, EstimatedCost: estimatedCost, Cost: t.Cost,
		Charged: t.Charged || t.Cost > 0, BalanceAfter: t.BalanceAfter, Status: t.Status, Error: t.Error,
	}
}

func (s *Store) updateTaskAudit(ctx context.Context, t *Task, action string, ok bool) {
	if err := s.AppendTaskAudit(ctx, taskAuditEntry(t, action, ok)); err != nil {
		log.Printf("task %s update audit: %v", t.ID, err)
	}
}

// CreateUploadingTask 建单（不扣点）：任务置 uploading，等客户端直传 TOS 后 submit。
func (s *Store) CreateUploadingTask(ctx context.Context, t *Task) error {
	c, err := s.GetCardByID(ctx, t.CardID)
	if err != nil {
		return err
	}
	t.MachineHash = c.MachineHash
	t.CardHash = c.Hash
	t.CardMasked = c.CodeMasked
	t.Status = TaskUploading
	now := s.Now().Unix()
	t.CreatedAt, t.UpdatedAt = now, now
	if _, err := s.cli.WithContext(ctx).CreateTypeIfAbsent(keyTask+t.ID, redimo.TypeString, 0, now); err != nil {
		return err
	}
	ok, err := s.cli.WithContext(ctx).SET(keyTask+t.ID, mustMarshal(t), redimo.IfNotExists)
	if err != nil {
		return err
	}
	if !ok {
		return ErrDuplicate
	}
	e := taskAuditEntry(t, "task_created", true)
	e.Actor = "client"
	if err := s.AppendTaskAudit(ctx, e); err != nil {
		log.Printf("task %s update audit: %v", t.ID, err)
	}
	return nil
}

// SubmitTaskWithDebit 提交点：uploading→processing 先记录拟扣费，再扣机器余额。
// 扣费失败时回滚到 uploading，但保留时长和拟扣费供审计与重试参考。
func (s *Store) SubmitTaskWithDebit(ctx context.Context, taskID string, durationSec, cost int64) (balanceAfter int64, err error) {
	t, err := s.GetTask(ctx, taskID)
	if err != nil {
		return 0, err
	}
	if t.Status != TaskUploading {
		return 0, ErrTaskState
	}
	ok, err := s.casTask(ctx, t, func(n *Task) {
		n.Status = TaskProcessing
		n.DurationSec = durationSec
		n.EstimatedCost = cost
		n.Cost = 0
		n.Charged = false
		n.BalanceAfter = nil
		n.SubmittedAt = s.Now().Unix()
	})
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, ErrTaskState
	}
	s.updateTaskAudit(ctx, t, "task_submitting", true)

	balanceAfter, err = s.DebitMachine(ctx, t.MachineHash, cost, "debit", t.ID, t.CardID)
	if err != nil {
		t.Status = TaskUploading
		t.Cost = 0
		t.Charged = false
		t.BalanceAfter = &balanceAfter
		if fresh, gerr := s.GetTask(ctx, taskID); gerr == nil {
			if _, rerr := s.casTask(ctx, fresh, func(n *Task) {
				n.Status = TaskUploading
				n.Cost = 0
				n.Charged = false
				n.BalanceAfter = &balanceAfter
				n.SubmittedAt = 0
			}); rerr != nil {
				log.Printf("task %s rollback to uploading failed: %v", taskID, rerr)
			}
		}
		t.Error = err.Error()
		s.updateTaskAudit(ctx, t, "task_debit_failed", false)
		return balanceAfter, err
	}

	marked, markErr := s.casTask(ctx, t, func(n *Task) {
		n.Cost = cost
		n.Charged = true
		n.BalanceAfter = &balanceAfter
	})
	if markErr != nil || !marked {
		log.Printf("task %s mark charged: ok=%v err=%v", taskID, marked, markErr)
		t.Cost = cost
		t.Charged = true
		t.BalanceAfter = &balanceAfter
	}
	s.updateTaskAudit(ctx, t, "task_debited", true)
	return balanceAfter, nil
}

// FailTaskWithRefund 仅当任务仍处 uploading/processing 时置 failed；
// processing 才退款（uploading 未扣点）。幂等。
func (s *Store) FailTaskWithRefund(ctx context.Context, taskID, errMsg string) error {
	t, err := s.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if t.Status != TaskUploading && t.Status != TaskProcessing {
		return nil // 已终态，不重复退款
	}
	wasCharged := t.Charged || t.Cost > 0
	ok, err := s.casTask(ctx, t, func(n *Task) {
		n.Status = TaskFailed
		n.Error = errMsg
		n.FinishedAt = s.Now().Unix()
	})
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("fail task: cas conflict")
	}
	if wasCharged && t.MachineHash != "" {
		balance, refundErr := s.CreditMachine(ctx, t.MachineHash, t.Cost, "refund", t.ID, t.CardID)
		if refundErr != nil {
			log.Printf("task %s refund failed: %v", t.ID, refundErr)
		} else {
			t.BalanceAfter = &balance
			if _, markErr := s.casTask(ctx, t, func(n *Task) {
				n.BalanceAfter = &balance
			}); markErr != nil {
				log.Printf("task %s record refund balance: %v", t.ID, markErr)
			}
		}
	}
	s.updateTaskAudit(ctx, t, "task_failed", true)
	return nil
}

func (s *Store) SetLasTaskID(ctx context.Context, id, lasTaskID string) error {
	t, err := s.GetTask(ctx, id)
	if err != nil {
		return err
	}
	ok, err := s.casTask(ctx, t, func(n *Task) { n.LasTaskID = lasTaskID })
	if err == nil && !ok {
		return errors.New("set las id: cas conflict")
	}
	return err
}

// CompleteTask 置 completed 并记录算子侧成片 URL。
func (s *Store) CompleteTask(ctx context.Context, id, resultURL string) error {
	t, err := s.GetTask(ctx, id)
	if err != nil {
		return err
	}
	ok, err := s.casTask(ctx, t, func(n *Task) {
		n.Status = TaskCompleted
		n.ResultURL = resultURL
		n.FinishedAt = s.Now().Unix()
	})
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("complete task: cas conflict")
	}
	s.updateTaskAudit(ctx, t, "task_completed", true)
	return nil
}
