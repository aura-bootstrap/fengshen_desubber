package ddbstore

import (
	"context"
	"encoding/json"
	"errors"
	"log"

	redimo "github.com/aura-studio/redimo/v2"
)

const (
	TaskQueued     = "queued"
	TaskProcessing = "processing"
	TaskCompleted  = "completed"
	TaskFailed     = "failed"
)

// Task 去字幕任务（落库 JSON 字段序固定=SETCAS 基线前提）。
// Provider 为处理平台名（provider.Registry 注册名），空 = 默认平台。
// CardID 为提交任务所用凭证卡（归因/可见性）；费用走 MachineHash 机器账户。
type Task struct {
	ID          string `json:"id"`
	CardID      int64  `json:"card_id"`
	CardHash    string `json:"card_hash"`
	MachineHash string `json:"machine_hash"`
	Provider    string `json:"provider"`
	SrcPath     string `json:"src_path"`
	ResultPath  string `json:"result_path,omitempty"`
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

// mutateIDList 整值 CAS 改写任务 id 列表键（queue/proc）。
func (s *Store) mutateIDList(ctx context.Context, key string, mutate func([]string) []string) error {
	for attempt := 0; attempt < 5; attempt++ {
		var cur []string
		var oldJSON string
		exists := false
		rv, err := s.cli.WithContext(ctx).GET(key)
		if err != nil {
			return err
		}
		if !rv.Empty() {
			exists = true
			oldJSON = rv.String()
			if err := json.Unmarshal([]byte(oldJSON), &cur); err != nil {
				return err
			}
		}
		newJSON := mustMarshal(mutate(cur))
		var ok bool
		if exists {
			ok, err = s.cli.WithContext(ctx).SETCAS(key, redimo.StringValue{S: newJSON}, redimo.StringValue{S: oldJSON}, true)
		} else {
			ok, err = s.cli.WithContext(ctx).SETCAS(key, redimo.StringValue{S: newJSON}, nil, false)
		}
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
	}
	return errors.New("task queue: cas conflict")
}

func removeID(ids []string, id string) []string {
	out := ids[:0]
	for _, v := range ids {
		if v != id {
			out = append(out, v)
		}
	}
	return out
}

// CreateTaskWithDebit 提交点=机器余额 CAS 扣费；随后落任务键 + 入队。
// 入队失败则退款并置失败，避免任务搁浅吞掉预扣。
func (s *Store) CreateTaskWithDebit(ctx context.Context, t *Task) (balanceAfter int64, err error) {
	c, err := s.GetCardByID(ctx, t.CardID)
	if err != nil {
		return 0, err
	}
	t.MachineHash = c.MachineHash
	balanceAfter, err = s.DebitMachine(ctx, t.MachineHash, t.Cost, "debit", t.ID, c.ID)
	if err != nil {
		return balanceAfter, err
	}
	now := s.Now().Unix()
	t.CardHash = c.Hash
	t.Status = TaskQueued
	t.CreatedAt, t.UpdatedAt = now, now
	if _, err := s.cli.WithContext(ctx).CreateTypeIfAbsent(keyTask+t.ID, redimo.TypeString, 0, now); err != nil {
		return balanceAfter, err
	}
	ok, err := s.cli.WithContext(ctx).SET(keyTask+t.ID, mustMarshal(t), redimo.IfNotExists)
	if err != nil {
		return balanceAfter, err
	}
	if !ok {
		return balanceAfter, ErrDuplicate
	}
	if err := s.mutateIDList(ctx, keyQueue, func(ids []string) []string { return append(ids, t.ID) }); err != nil {
		log.Printf("task %s enqueue failed, refunding: %v", t.ID, err)
		if ferr := s.FailTaskWithRefund(ctx, t.ID, "enqueue failed"); ferr != nil {
			log.Printf("task %s enqueue-fail refund failed: %v", t.ID, ferr)
		}
		return balanceAfter, err
	}
	return balanceAfter, nil
}

// FailTaskWithRefund 仅当任务仍处于 queued/processing 时置 failed 并退款，幂等。
func (s *Store) FailTaskWithRefund(ctx context.Context, taskID, errMsg string) error {
	t, err := s.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if t.Status != TaskQueued && t.Status != TaskProcessing {
		return nil // 已终态，不重复退款
	}
	ok, err := s.casTask(ctx, t, func(n *Task) { n.Status = TaskFailed; n.Error = errMsg })
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("fail task: cas conflict")
	}
	machine := t.MachineHash
	if machine == "" {
		// 旧模型任务（无机器字段）：经卡解析机器
		if c, cerr := s.GetCardByID(ctx, t.CardID); cerr == nil {
			machine = c.MachineHash
		}
	}
	if machine != "" {
		if _, err := s.CreditMachine(ctx, machine, t.Cost, "refund", t.ID, t.CardID); err != nil {
			log.Printf("task %s refund failed: %v", t.ID, err)
		}
	}
	if err := s.mutateIDList(ctx, keyQueue, func(ids []string) []string { return removeID(ids, t.ID) }); err != nil {
		log.Printf("task %s dequeue failed: %v", t.ID, err)
	}
	if err := s.mutateIDList(ctx, keyProc, func(ids []string) []string { return removeID(ids, t.ID) }); err != nil {
		log.Printf("task %s deproc failed: %v", t.ID, err)
	}
	return nil
}

// NextQueued 取出队首任务并置 processing（queue→proc 迁移）。空队列返回 nil,nil。
func (s *Store) NextQueued(ctx context.Context) (*Task, error) {
	for attempt := 0; attempt < 5; attempt++ {
		rv, err := s.cli.WithContext(ctx).GET(keyQueue)
		if err != nil {
			return nil, err
		}
		var ids []string
		if !rv.Empty() {
			if err := json.Unmarshal([]byte(rv.String()), &ids); err != nil {
				return nil, err
			}
		}
		if len(ids) == 0 {
			return nil, nil
		}
		id := ids[0]
		t, err := s.GetTask(ctx, id)
		if errors.Is(err, ErrNotFound) {
			// 陈旧条目：踢出队列重试
			if err := s.mutateIDList(ctx, keyQueue, func(l []string) []string { return removeID(l, id) }); err != nil {
				return nil, err
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		if t.Status != TaskQueued {
			if err := s.mutateIDList(ctx, keyQueue, func(l []string) []string { return removeID(l, id) }); err != nil {
				return nil, err
			}
			continue
		}
		if err := s.mutateIDList(ctx, keyQueue, func(l []string) []string { return removeID(l, id) }); err != nil {
			return nil, err
		}
		ok, err := s.casTask(ctx, t, func(n *Task) { n.Status = TaskProcessing })
		if err != nil {
			return nil, err
		}
		if !ok {
			continue // 并发下状态已变，跳过
		}
		if err := s.mutateIDList(ctx, keyProc, func(l []string) []string { return append(l, id) }); err != nil {
			log.Printf("task %s proc-push failed: %v", id, err)
		}
		return t, nil
	}
	return nil, errors.New("next queued: cas conflict")
}

// ResetProcessing 启动恢复：proc 列表中的任务置回 queued 并排回队首。
func (s *Store) ResetProcessing(ctx context.Context) error {
	rv, err := s.cli.WithContext(ctx).GET(keyProc)
	if err != nil {
		return err
	}
	var ids []string
	if rv.Empty() {
		return nil
	}
	if err := json.Unmarshal([]byte(rv.String()), &ids); err != nil {
		return err
	}
	for _, id := range ids {
		t, err := s.GetTask(ctx, id)
		if err != nil {
			continue
		}
		if t.Status != TaskProcessing {
			continue
		}
		if _, err := s.casTask(ctx, t, func(n *Task) { n.Status = TaskQueued }); err != nil {
			return err
		}
		if err := s.mutateIDList(ctx, keyQueue, func(l []string) []string { return append([]string{id}, l...) }); err != nil {
			return err
		}
	}
	return s.mutateIDList(ctx, keyProc, func([]string) []string { return nil })
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

func (s *Store) CompleteTask(ctx context.Context, id, resultPath string) error {
	t, err := s.GetTask(ctx, id)
	if err != nil {
		return err
	}
	ok, err := s.casTask(ctx, t, func(n *Task) { n.Status = TaskCompleted; n.ResultPath = resultPath })
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("complete task: cas conflict")
	}
	if err := s.mutateIDList(ctx, keyProc, func(l []string) []string { return removeID(l, id) }); err != nil {
		log.Printf("task %s deproc failed: %v", id, err)
	}
	return nil
}
