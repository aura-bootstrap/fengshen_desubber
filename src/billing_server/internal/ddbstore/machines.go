package ddbstore

import (
	"context"
	"encoding/json"
	"errors"
	"log"

	redimo "github.com/aura-studio/redimo/v2"
)

// Machine 机器账户：点数记在机器上。卡=一次性充值券，激活即核销，
// 卡面点数全部转入机器账户；同一机器可叠充多卡，余额累计（落库 JSON 字段序固定=SETCAS 基线前提）。
type Machine struct {
	MachineHash string `json:"machine_hash"`
	Balance     int64  `json:"balance"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

func (s *Store) GetMachine(ctx context.Context, hash string) (*Machine, error) {
	rv, err := s.cli.WithContext(ctx).GET(keyMach + hash)
	if err != nil {
		return nil, err
	}
	if rv.Empty() {
		return nil, ErrNotFound
	}
	var m Machine
	if err := json.Unmarshal([]byte(rv.String()), &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// ensureMachine 取机器账户，不存在则建 0 余额账户。
func (s *Store) ensureMachine(ctx context.Context, hash string) (*Machine, error) {
	m, err := s.GetMachine(ctx, hash)
	if err == nil {
		return m, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	now := s.Now().Unix()
	m = &Machine{MachineHash: hash, CreatedAt: now, UpdatedAt: now}
	if _, err := s.cli.WithContext(ctx).CreateTypeIfAbsent(keyMach+hash, redimo.TypeString, 0, now); err != nil {
		return nil, err
	}
	if _, err := s.cli.WithContext(ctx).SET(keyMach+hash, mustMarshal(m), redimo.IfNotExists); err != nil {
		return nil, err
	}
	// 并发同建：以库内值为准
	return s.GetMachine(ctx, hash)
}

// casMachine 整值 CAS 改写机器账户，有限重试。成功后 m 指向新值。
func (s *Store) casMachine(ctx context.Context, m *Machine, mutate func(*Machine)) (bool, error) {
	for attempt := 0; attempt < 5; attempt++ {
		oldJSON := mustMarshal(m)
		next := *m
		next.UpdatedAt = s.Now().Unix()
		mutate(&next)
		ok, err := s.cli.WithContext(ctx).SETCAS(keyMach+m.MachineHash,
			redimo.StringValue{S: mustMarshal(&next)}, redimo.StringValue{S: oldJSON}, true)
		if err != nil {
			return false, err
		}
		if ok {
			*m = next
			return true, nil
		}
		fresh, err := s.GetMachine(ctx, m.MachineHash)
		if err != nil {
			return false, err
		}
		*m = *fresh
	}
	return false, nil
}

// CreditMachine 入账（激活转账 grant / 退款 refund / admin 充值）。余额 CAS 为提交点，流水追加留痕。
// cardID 为触发来源卡（流水归因用，可为 0）。
func (s *Store) CreditMachine(ctx context.Context, hash string, amount int64, kind, ref string, cardID int64) (int64, error) {
	m, err := s.ensureMachine(ctx, hash)
	if err != nil {
		return 0, err
	}
	ok, err := s.casMachine(ctx, m, func(n *Machine) { n.Balance += amount })
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, errors.New("credit machine: cas conflict")
	}
	_ = s.AppendMachineTx(ctx, hash, cardID, ref, kind, amount, m.Balance)
	return m.Balance, nil
}

// DebitMachine 扣费：机器余额须充足。成功返回新余额。
func (s *Store) DebitMachine(ctx context.Context, hash string, amount int64, kind, ref string, cardID int64) (int64, error) {
	m, err := s.GetMachine(ctx, hash)
	if errors.Is(err, ErrNotFound) {
		return 0, ErrInsufficientBalance
	}
	if err != nil {
		return 0, err
	}
	if m.Balance < amount {
		return m.Balance, ErrInsufficientBalance
	}
	ok, err := s.casMachine(ctx, m, func(n *Machine) { n.Balance -= amount })
	if err != nil {
		return 0, err
	}
	if !ok {
		return m.Balance, ErrInsufficientBalance // 并发冲突按不足处理，调用方重读重试
	}
	_ = s.AppendMachineTx(ctx, hash, cardID, ref, kind, amount, m.Balance)
	return m.Balance, nil
}

// uncreditMachine 激活冲正：卡核销 CAS 失败时把已入账的点数减回。
// 余额不足时减到 0 为止并记审计差异（并发扣费挤占的极端情形）。
func (s *Store) uncreditMachine(ctx context.Context, hash string, amount int64) {
	if amount <= 0 {
		return
	}
	m, err := s.GetMachine(ctx, hash)
	if err != nil {
		log.Printf("uncredit machine %s: read failed: %v", hash[:12], err)
		return
	}
	var short int64
	ok, err := s.casMachine(ctx, m, func(n *Machine) {
		if n.Balance < amount {
			short = amount - n.Balance
			n.Balance = 0
		} else {
			n.Balance -= amount
		}
	})
	if err != nil || !ok {
		log.Printf("uncredit machine %s: cas failed: %v", hash[:12], err)
		return
	}
	_ = s.AppendMachineTx(ctx, hash, 0, "", "reversal", amount, m.Balance)
	if short > 0 {
		_ = s.AppendAudit(ctx, AuditEntry{Actor: "system", Action: "uncredit-short", Target: hash[:12],
			Detail: "short by concurrent debit", OK: false})
	}
}
