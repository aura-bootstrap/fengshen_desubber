package ddbstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// CreditTx 资金流水。JSON 字段名（ID/UserID/TaskID/...）是 /v1/admin/transactions
// 的冻结协议形态，不得加 tag 改名。UserID 现为卡 id。
type CreditTx struct {
	ID               int64
	UserID           int64
	TaskID           string
	Kind             string // debit|refund|grant
	Amount           int64
	BalanceAfter     int64
	CreatedAt        time.Time
	OriginalFilename string `json:"OriginalFilename,omitempty"`
}

// AppendMachineTx 追加机器账户流水（Hash field=auditSK，追加序=时间序）。
// 键 tx:mach:<machine_hash>；UserID 字段记触发来源卡 id（归因用，可为 0）。
// 追加失败由调用方记日志（D1=A）。
func (s *Store) AppendMachineTx(ctx context.Context, machineHash string, cardID int64, ref, kind string, amount, balanceAfter int64) error {
	id, err := s.cli.WithContext(ctx).INCR(keySeq + "tx")
	if err != nil {
		return err
	}
	t := CreditTx{
		ID: id, UserID: cardID, TaskID: ref, Kind: kind,
		Amount: amount, BalanceAfter: balanceAfter, CreatedAt: s.Now(),
	}
	_, err = s.cli.WithContext(ctx).HSET(keyTx+"mach:"+machineHash, auditSK(s.Now().Unix()), mustMarshal(t))
	return err
}

// ImportTx 迁移契约（cmd/migrate 专用）：按原样写入一条流水（保留源库 id/时间）。
// field 由源 id 派生（去掉随机段），重复执行覆盖同字段=幂等。
func (s *Store) ImportTx(ctx context.Context, t CreditTx) error {
	field := fmt.Sprintf("%013d#%010d#mig", t.CreatedAt.Unix(), t.ID)
	_, err := s.cli.WithContext(ctx).HSET(keyTx+itoa(t.UserID), field, mustMarshal(t))
	return err
}

// ListMachineTx 读某机器整条流水（HScanPage 基表 Query，sk 升序=时间升序）。
func (s *Store) ListMachineTx(ctx context.Context, machineHash string) ([]CreditTx, error) {
	key := keyTx + "mach:" + machineHash
	out := make([]CreditTx, 0, 64)
	var start map[string]types.AttributeValue
	for pages := 0; pages < 100; pages++ {
		fields, last, err := s.cli.WithContext(ctx).HScanPage(key, 0, start)
		if err != nil {
			return nil, err
		}
		for _, f := range fields {
			var t CreditTx
			if err := json.Unmarshal([]byte(f.Value.String()), &t); err != nil {
				return nil, err
			}
			out = append(out, t)
		}
		if len(last) == 0 {
			break
		}
		start = last
	}
	return out, nil
}
