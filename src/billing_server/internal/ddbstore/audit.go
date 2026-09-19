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

// AuditEntry 审计条目（卡相关资金/状态变更）。
type AuditEntry struct {
	TS     int64  `json:"ts"`
	Actor  string `json:"actor"`
	Action string `json:"action"`
	Target string `json:"target"`
	Detail string `json:"detail,omitempty"`
	OK     bool   `json:"ok"`
}

// AppendAudit 向 audit:card 追加一条。失败由调用方记日志，不回滚业务态（D1=A）。
func (s *Store) AppendAudit(ctx context.Context, e AuditEntry) error {
	e.TS = s.Now().Unix()
	_, err := s.cli.WithContext(ctx).HSET(keyAudit+"card", auditSK(e.TS), mustMarshal(e))
	return err
}

// AuditList 读整条卡审计链（sk 升序=时间升序），每条附 sk 字段。
func (s *Store) AuditList(ctx context.Context) ([]map[string]any, error) {
	key := keyAudit + "card"
	out := make([]map[string]any, 0, 64)
	var start map[string]types.AttributeValue
	for pages := 0; pages < 100; pages++ {
		fields, last, err := s.cli.WithContext(ctx).HScanPage(key, 0, start)
		if err != nil {
			return nil, err
		}
		for _, f := range fields {
			var m map[string]any
			if err := json.Unmarshal([]byte(f.Value.String()), &m); err != nil {
				return nil, err
			}
			m["sk"] = f.Field
			out = append(out, m)
		}
		if len(last) == 0 {
			break
		}
		start = last
	}
	return out, nil
}
