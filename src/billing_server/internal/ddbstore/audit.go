package ddbstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

var seqCounter atomic.Int64

// auditSK 13 位补零 ts 前缀使 sk 字节序=时间序，追加序天然保序。
func auditSK(ts int64) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%013d#%010d#%s", ts, seqCounter.Add(1), hex.EncodeToString(b))
}

// AuditEntry 审计条目。卡/账号管理行为使用基础字段；云任务使用按任务 ID 更新的结构化快照。
type AuditEntry struct {
	TS            int64  `json:"ts"`
	UpdatedAt     int64  `json:"updated_at,omitempty"`
	Actor         string `json:"actor"`
	Action        string `json:"action"`
	Target        string `json:"target"`
	Detail        string `json:"detail,omitempty"`
	OK            bool   `json:"ok"`
	TaskID        string `json:"task_id,omitempty"`
	MachineHash   string `json:"machine_hash,omitempty"`
	CardID        int64  `json:"card_id,omitempty"`
	CardMasked    string `json:"card_masked,omitempty"`
	Provider      string `json:"provider,omitempty"`
	SourceName    string `json:"source_name,omitempty"`
	SourcePath    string `json:"source_path,omitempty"`
	DurationSec   int64  `json:"duration_sec,omitempty"`
	EstimatedCost int64  `json:"estimated_cost,omitempty"`
	Cost          int64  `json:"cost,omitempty"`
	Charged       bool   `json:"charged,omitempty"`
	BalanceAfter  *int64 `json:"balance_after,omitempty"`
	Status        string `json:"status,omitempty"`
	Error         string `json:"error,omitempty"`
}

// AppendAudit 向 audit:card 追加一条。失败由调用方记日志，不回滚业务态（D1=A）。
func (s *Store) AppendAudit(ctx context.Context, e AuditEntry) error {
	return s.appendAudit(ctx, "card", e)
}

// AppendTaskAudit 按任务 ID 写入同一条实时快照。
func (s *Store) AppendTaskAudit(ctx context.Context, e AuditEntry) error {
	if e.TaskID == "" {
		return fmt.Errorf("task audit: empty task id")
	}
	now := s.Now().Unix()
	if e.TS == 0 {
		e.TS = now
	}
	e.UpdatedAt = now
	_, err := s.cli.WithContext(ctx).HSET(keyAudit+"task", "task:"+e.TaskID, mustMarshal(e))
	return err
}

func (s *Store) appendAudit(ctx context.Context, scope string, e AuditEntry) error {
	e.TS = s.Now().Unix()
	_, err := s.cli.WithContext(ctx).HSET(keyAudit+scope, auditSK(e.TS), mustMarshal(e))
	return err
}

// AuditList 合并卡/账号管理与云任务审计链，每条附 sk/scope 字段。
func (s *Store) AuditList(ctx context.Context) ([]map[string]any, error) {
	out := make([]map[string]any, 0, 128)
	for _, scope := range []string{"card", "task"} {
		var start map[string]types.AttributeValue
		for pages := 0; pages < 100; pages++ {
			fields, last, err := s.cli.WithContext(ctx).HScanPage(keyAudit+scope, 0, start)
			if err != nil {
				return nil, err
			}
			for _, f := range fields {
				var m map[string]any
				if err := json.Unmarshal([]byte(f.Value.String()), &m); err != nil {
					return nil, err
				}
				m["sk"] = f.Field
				m["scope"] = scope
				out = append(out, m)
			}
			if len(last) == 0 {
				break
			}
			start = last
		}
	}
	return out, nil
}
