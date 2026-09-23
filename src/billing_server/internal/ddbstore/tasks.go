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
	TaskSettling   = "settling"
	TaskCompleted  = "completed"
	TaskFailed     = "failed"
)

// Task 去字幕任务（落库 JSON 字段序固定=SETCAS 基线前提）。
// Provider 为处理平台名（provider.Registry 注册名），空 = 默认平台。
// CardID 为提交任务所用凭证卡（归因/可见性）；费用走 MachineHash 机器账户。
// SrcKey 为 TOS 输入对象键（客户端直传）；ResultURL 为算子侧成片地址（服务端不中转）。
type Task struct {
	ID               string `json:"id"`
	CardID           int64  `json:"card_id"`
	CardHash         string `json:"card_hash"`
	MachineHash      string `json:"machine_hash"`
	Provider         string `json:"provider"`
	SrcKey           string `json:"src_key"`
	ResultURL        string `json:"result_url,omitempty"`
	DurationSec      int64  `json:"duration_sec"`
	Cost             int64  `json:"cost"`
	Status           string `json:"status"`
	Error            string `json:"error,omitempty"`
	LasTaskID        string `json:"las_task_id,omitempty"`
	CreatedAt        int64  `json:"created_at"`
	UpdatedAt        int64  `json:"updated_at"`
	OriginalFilename string `json:"original_filename,omitempty"`
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

// CreateUploadingTask 建单（不扣点）：任务置 uploading，等客户端直传 TOS 后 submit。
// CardID=0 表示开发版管理员内部任务：只记录机器码归因，不绑定卡账户。
func (s *Store) CreateUploadingTask(ctx context.Context, t *Task) error {
	if t.CardID != 0 {
		c, err := s.GetCardByID(ctx, t.CardID)
		if err != nil {
			return err
		}
		t.MachineHash = c.MachineHash
		t.CardHash = c.Hash
	}
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
	return nil
}

func (s *Store) StartTask(ctx context.Context, taskID string) (int64, error) {
	t, err := s.GetTask(ctx, taskID)
	if err != nil {
		return 0, err
	}
	if t.Status != TaskUploading {
		return 0, ErrTaskState
	}
	balance := int64(0)
	if t.CardID != 0 {
		m, err := s.GetMachine(ctx, t.MachineHash)
		if err != nil || m.Balance < 1 {
			return 0, ErrInsufficientBalance
		}
		balance = m.Balance
	}
	ok, err := s.casTask(ctx, t, func(n *Task) { n.Status = TaskProcessing })
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, ErrTaskState
	}
	return balance, nil
}

func (s *Store) FinalizeTaskWithDebit(ctx context.Context, taskID string, durationSec, cost int64, resultURL string) (int64, error) {
	t, err := s.GetTask(ctx, taskID)
	if err != nil {
		return 0, err
	}
	if t.Status != TaskProcessing {
		return 0, ErrTaskState
	}
	if t.CardID == 0 {
		ok, err := s.casTask(ctx, t, func(n *Task) {
			n.Status = TaskCompleted
			n.DurationSec = durationSec
			n.Cost = 0
			n.ResultURL = resultURL
		})
		if err != nil {
			return 0, err
		}
		if !ok {
			return 0, ErrTaskState
		}
		return 0, nil
	}
	ok, err := s.casTask(ctx, t, func(n *Task) {
		n.Status = TaskSettling
		n.DurationSec = durationSec
		n.Cost = cost
		n.ResultURL = resultURL
	})
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, ErrTaskState
	}
	balanceAfter, err := s.DebitMachine(ctx, t.MachineHash, cost, "debit", t.ID, t.CardID)
	if err != nil {
		fresh, getErr := s.GetTask(ctx, taskID)
		if getErr == nil {
			_, _ = s.casTask(ctx, fresh, func(n *Task) {
				if errors.Is(err, ErrInsufficientBalance) {
					n.Status = TaskFailed
					n.Error = "insufficient balance at settlement"
					n.ResultURL = ""
				} else {
					n.Status = TaskProcessing
					n.DurationSec = 0
					n.Cost = 0
					n.ResultURL = ""
				}
			})
		}
		return balanceAfter, err
	}
	fresh, err := s.GetTask(ctx, taskID)
	if err != nil {
		return balanceAfter, err
	}
	ok, err = s.casTask(ctx, fresh, func(n *Task) { n.Status = TaskCompleted })
	if err != nil {
		return balanceAfter, err
	}
	if !ok {
		return balanceAfter, errors.New("finalize task: cas conflict")
	}
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
	wasProcessing := t.Status == TaskProcessing
	ok, err := s.casTask(ctx, t, func(n *Task) { n.Status = TaskFailed; n.Error = errMsg })
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("fail task: cas conflict")
	}
	if wasProcessing && t.CardID != 0 && t.MachineHash != "" && t.Cost > 0 {
		if _, err := s.CreditMachine(ctx, t.MachineHash, t.Cost, "refund", t.ID, t.CardID); err != nil {
			log.Printf("task %s refund failed: %v", t.ID, err)
		}
	}
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
	ok, err := s.casTask(ctx, t, func(n *Task) { n.Status = TaskCompleted; n.ResultURL = resultURL })
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("complete task: cas conflict")
	}
	return nil
}
