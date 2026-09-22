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
	ID          string `json:"id"`
	CardID      int64  `json:"card_id"`
	CardHash    string `json:"card_hash"`
	MachineHash string `json:"machine_hash"`
	Provider    string `json:"provider"`
	SrcKey      string `json:"src_key"`
	ResultURL   string `json:"result_url,omitempty"`
	DurationSec int64  `json:"duration_sec"`
	Cost        int64  `json:"cost"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
	LasTaskID   string `json:"las_task_id,omitempty"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
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
func (s *Store) CreateUploadingTask(ctx context.Context, t *Task) error {
	c, err := s.GetCardByID(ctx, t.CardID)
	if err != nil {
		return err
	}
	t.MachineHash = c.MachineHash
	t.CardHash = c.Hash
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

// SubmitTaskWithDebit 提交点：uploading→processing 先 CAS 占位（防重复提交），
// 再机器余额扣费；扣费失败（如余额不足）回滚状态到 uploading，任务可重试。
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
		n.Cost = cost
	})
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, ErrTaskState
	}
	balanceAfter, err = s.DebitMachine(ctx, t.MachineHash, cost, "debit", t.ID, t.CardID)
	if err != nil {
		if fresh, gerr := s.GetTask(ctx, taskID); gerr == nil {
			if _, rerr := s.casTask(ctx, fresh, func(n *Task) { n.Status = TaskUploading }); rerr != nil {
				log.Printf("task %s rollback to uploading failed: %v", taskID, rerr)
			}
		}
		return balanceAfter, err
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
	if wasProcessing && t.MachineHash != "" {
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
