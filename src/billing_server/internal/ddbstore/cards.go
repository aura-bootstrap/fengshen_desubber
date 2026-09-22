package ddbstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	redimo "github.com/aura-studio/redimo/v2"

	"fengshen-desubber/billing_server/internal/cardkey"
)

const (
	CardInactive = "inactive" // 新卡未激活（未核销，点数还在卡面上）
	CardRedeemed = "redeemed" // 已核销（点数已转入机器账户，卡面仅作凭证）
	CardRevoked  = "revoked"  // 吊销
	// CardActive 旧模型遗留状态（余额在卡上）。不再产生，
	// 读取路径（auth/activate）命中时懒迁移为 redeemed。
	CardActive = "active"
)

func (s *Store) AllocateBatch(ctx context.Context, batch string) (string, error) {
	requested, numeric := int64(0), false
	if batch != "" {
		if n, err := strconv.ParseInt(batch, 10, 64); err == nil && n >= 0 && strconv.FormatInt(n, 10) == batch {
			requested, numeric = n, true
		} else {
			return batch, nil
		}
	}
	for attempt := 0; attempt < 5; attempt++ {
		rv, err := s.cli.WithContext(ctx).GET(keySeq + "batch")
		if err != nil {
			return "", err
		}
		cur, exists := int64(0), !rv.Empty()
		if exists {
			cur, err = strconv.ParseInt(rv.String(), 10, 64)
			if err != nil {
				return "", err
			}
		}
		if numeric && requested <= cur {
			return batch, nil
		}
		next := cur + 1
		if numeric {
			next = requested
		}
		ok, err := s.cli.WithContext(ctx).SETCAS(
			keySeq+"batch", redimo.IntValue{I: next}, redimo.IntValue{I: cur}, exists,
		)
		if err != nil {
			return "", err
		}
		if ok {
			return strconv.FormatInt(next, 10), nil
		}
	}
	return "", errors.New("batch counter conflict")
}

// Card 卡账户：卡=一次性充值券，激活即核销，点数转入机器账户（见 machines.go）。
// 核销后卡面仅作调用凭证（MachineHash 映射保留）；Balance 核销后恒 0，余额查机器账户。
// MachineHash 空 = 未绑定机器；BoundAt 为首次绑定时间（unix 秒）；
// Credited = 点数已转入机器账户（核销与迁移的完成标记）。
// （落库 JSON 字段序固定=SETCAS 基线前提；新增字段只追加在末尾。）
type Card struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	CodeMasked  string `json:"code_masked"`
	Hash        string `json:"hash"`
	Batch       string `json:"batch"`
	Balance     int64  `json:"balance"`
	Status      string `json:"status"`
	MachineHash string `json:"machine_hash"` // 空=未绑定
	BoundAt     int64  `json:"bound_at"`
	CreatedAt   int64  `json:"created_at"`
	Credited    bool   `json:"credited"`
}

// CreateCard 发卡（明文卡面仅由调用方返回一次，库中只存哈希）。新卡默认 inactive。
func (s *Store) CreateCard(ctx context.Context, name, code string, credits int64, batch string) (*Card, error) {
	h, err := cardkey.HashV2(s.Pepper, code)
	if err != nil {
		return nil, err
	}
	return s.importCard(ctx, name, code, h, credits, batch)
}

// importCard 按既定 hash 落卡（迁移路径直接沿用源库 hash，无需明文卡面）。默认 inactive。
func (s *Store) importCard(ctx context.Context, name, code, hash string, credits int64, batch string) (*Card, error) {
	id, err := s.cli.WithContext(ctx).INCR(keySeq + "card")
	if err != nil {
		return nil, err
	}
	c := &Card{
		ID: id, Name: name, CodeMasked: cardkey.Mask(code), Hash: hash,
		Batch: batch, Balance: credits, Status: CardInactive, CreatedAt: s.Now().Unix(),
	}
	key := keyCard + hash
	if _, err := s.cli.WithContext(ctx).CreateTypeIfAbsent(key, redimo.TypeString, 0, c.CreatedAt); err != nil {
		return nil, err
	}
	ok, err := s.cli.WithContext(ctx).SET(key, mustMarshal(c), redimo.IfNotExists)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrDuplicate
	}
	if _, err := s.cli.WithContext(ctx).CreateTypeIfAbsent(keyCardID+itoa(id), redimo.TypeString, 0, c.CreatedAt); err != nil {
		return nil, err
	}
	if _, err := s.cli.WithContext(ctx).SET(keyCardID+itoa(id), hash, redimo.IfNotExists); err != nil {
		return nil, err
	}
	if batch != "" {
		if _, err := s.cli.WithContext(ctx).HSET(keyCardIdx+batch, hash[:12], hash); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func (s *Store) GetCardByHash(ctx context.Context, hash string) (*Card, error) {
	rv, err := s.cli.WithContext(ctx).GET(keyCard + hash)
	if err != nil {
		return nil, err
	}
	if rv.Empty() {
		return nil, ErrCardNotFound
	}
	var c Card
	if err := json.Unmarshal([]byte(rv.String()), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// ImportCard 迁移契约：按既定 hash/时间落卡。status 显式传入优先，
// 空串按 inactive 处理（新卡未激活语义）。
// 返回 created=false 表示已存在（幂等跳过）。
func (s *Store) ImportCard(ctx context.Context, name, codeHint, hash string, credits int64, batch, status string, createdAt int64) (bool, error) {
	id, err := s.cli.WithContext(ctx).INCR(keySeq + "card")
	if err != nil {
		return false, err
	}
	masked := codeHint
	if masked == "" {
		masked = hash[:12] + "…"
	}
	if status == "" {
		status = CardInactive
	}
	c := &Card{
		ID: id, Name: name, CodeMasked: masked, Hash: hash,
		Batch: batch, Balance: credits, Status: status, CreatedAt: createdAt,
	}
	key := keyCard + hash
	if _, err := s.cli.WithContext(ctx).CreateTypeIfAbsent(key, redimo.TypeString, 0, createdAt); err != nil {
		return false, err
	}
	ok, err := s.cli.WithContext(ctx).SET(key, mustMarshal(c), redimo.IfNotExists)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	if _, err := s.cli.WithContext(ctx).CreateTypeIfAbsent(keyCardID+itoa(id), redimo.TypeString, 0, createdAt); err != nil {
		return false, err
	}
	if _, err := s.cli.WithContext(ctx).SET(keyCardID+itoa(id), hash, redimo.IfNotExists); err != nil {
		return false, err
	}
	if batch != "" {
		if _, err := s.cli.WithContext(ctx).HSET(keyCardIdx+batch, hash[:12], hash); err != nil {
			return false, err
		}
	}
	return true, nil
}

// GetCardByCode 按明文卡面查找：依次试 v2(HMAC)/v1(裸SHA256) 候选哈希。
func (s *Store) GetCardByCode(ctx context.Context, code string) (*Card, error) {
	hashes, err := cardkey.CandidateHashes(s.Pepper, code)
	if err != nil {
		return nil, err
	}
	for _, h := range hashes {
		c, err := s.GetCardByHash(ctx, h)
		if err == nil {
			return c, nil
		}
		if !errors.Is(err, ErrCardNotFound) {
			return nil, err
		}
	}
	return nil, ErrCardNotFound
}

func (s *Store) GetCardByID(ctx context.Context, id int64) (*Card, error) {
	rv, err := s.cli.WithContext(ctx).GET(keyCardID + itoa(id))
	if err != nil {
		return nil, err
	}
	if rv.Empty() {
		return nil, ErrCardNotFound
	}
	return s.GetCardByHash(ctx, rv.String())
}

// casCard 整值 CAS 改写卡，有限重试。成功后 c 指向新值。
func (s *Store) casCard(ctx context.Context, c *Card, mutate func(*Card)) (bool, error) {
	for attempt := 0; attempt < 5; attempt++ {
		oldJSON := mustMarshal(c)
		next := *c
		mutate(&next)
		ok, err := s.cli.WithContext(ctx).SETCAS(keyCard+c.Hash,
			redimo.StringValue{S: mustMarshal(&next)}, redimo.StringValue{S: oldJSON}, true)
		if err != nil {
			return false, err
		}
		if ok {
			*c = next
			return true, nil
		}
		fresh, err := s.GetCardByHash(ctx, c.Hash)
		if err != nil {
			return false, err
		}
		*c = *fresh
	}
	return false, nil
}

// EnsureRedeemed 核销/迁移统一入口：把卡面点数转入机器账户并把卡置 redeemed。
// 幂等：已 redeemed 且已 Credited 直接返回；redeemed 但未 Credited（旧迁移半成品）补转入。
// 返回机器最新余额。并发安全：入账在前、核销 CAS 在后，CAS 失败者冲正已入账点数，
// 无论多少并发激活同一张卡，机器账户净入账恰好一份卡面点数。
func (s *Store) EnsureRedeemed(ctx context.Context, c *Card, machineHash string) (int64, error) {
	if c.Status == CardRedeemed && c.Credited {
		m, err := s.GetMachine(ctx, machineHash)
		if err != nil {
			return 0, err
		}
		return m.Balance, nil
	}
	moved := c.Balance
	if moved > 0 {
		if _, err := s.CreditMachine(ctx, machineHash, moved, "grant", "redeem", c.ID); err != nil {
			return 0, err
		}
	}
	ok, err := s.casCard(ctx, c, func(n *Card) {
		n.Status = CardRedeemed
		if n.MachineHash == "" {
			n.MachineHash = machineHash
			n.BoundAt = s.Now().Unix()
		}
		n.Balance = 0
		n.Credited = true
	})
	if err != nil {
		s.uncreditMachine(ctx, machineHash, moved)
		return 0, err
	}
	if !ok {
		// 并发核销已抢先完成：冲正本进程多入账的那份，按已成功处理
		s.uncreditMachine(ctx, machineHash, moved)
		fresh, rerr := s.GetCardByHash(ctx, c.Hash)
		if rerr != nil {
			return 0, rerr
		}
		*c = *fresh
		if c.Status != CardRedeemed || c.MachineHash != machineHash {
			return 0, errors.New("redeem: cas conflict")
		}
	} else {
		_ = s.AppendAudit(ctx, AuditEntry{Actor: "card", Action: "redeem", Target: c.CodeMasked,
			Detail: fmt.Sprintf("machine=%s moved=%d", machineHash, moved), OK: true})
	}
	m, err := s.ensureMachine(ctx, machineHash) // 0 点数卡核销也要建档
	if err != nil {
		return 0, err
	}
	return m.Balance, nil
}

// UnbindCard 解绑机器码：仅对旧模型 active 卡有效（清绑定回 inactive，点数还在卡面）。
// redeemed 卡点数已转入机器账户，解绑不能复活卡面，返回 ErrCardRedeemed；
// revoked 卡拒绝；未绑定的 inactive 卡幂等通过。
func (s *Store) UnbindCard(ctx context.Context, hash, actor string) error {
	c, err := s.GetCardByHash(ctx, hash)
	if err != nil {
		return err
	}
	if c.Status == CardRevoked {
		return ErrCardRevoked
	}
	if c.Status == CardRedeemed {
		return ErrCardRedeemed
	}
	if c.Status == CardInactive && c.MachineHash == "" {
		return nil
	}
	ok, err := s.casCard(ctx, c, func(n *Card) {
		n.Status = CardInactive
		n.MachineHash = ""
		n.BoundAt = 0
	})
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("unbind: cas conflict")
	}
	_ = s.AppendAudit(ctx, AuditEntry{Actor: actor, Action: "unbind", Target: c.CodeMasked, OK: true})
	return nil
}

// SetCardStatus 吊销/恢复（同步批次索引 + 审计）。
// 吊销保留 MachineHash（凭证→机器映射，恢复后仍可用）。
// 恢复目标态按 Credited 区分：点数已转机器的回 redeemed，未核销的回 inactive。
func (s *Store) SetCardStatus(ctx context.Context, hash, status, actor string) error {
	c, err := s.GetCardByHash(ctx, hash)
	if err != nil {
		return err
	}
	if status == CardInactive && c.Credited {
		status = CardRedeemed
	}
	if c.Status == status {
		return nil
	}
	ok, err := s.casCard(ctx, c, func(n *Card) {
		n.Status = status
		if status == CardInactive {
			// 回到未核销态才允许清绑定（点数随卡面重新可激活）
			n.MachineHash = ""
			n.BoundAt = 0
		}
	})
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("card status: cas conflict")
	}
	action := "unrevoke"
	if status == CardRevoked {
		action = "revoke"
	}
	_ = s.AppendAudit(ctx, AuditEntry{Actor: actor, Action: action, Target: c.CodeMasked, OK: true})
	return nil
}

// RevokeBatch 整批吊销/恢复，返回处理数量。
func (s *Store) RevokeBatch(ctx context.Context, batch, status, actor string) (int, error) {
	fields, _, err := s.cli.WithContext(ctx).HScanPage(keyCardIdx+batch, 0, nil)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, f := range fields {
		if err := s.SetCardStatus(ctx, f.Value.String(), status, actor); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// ListCards 枚举卡：batch 非空走批次索引，否则扫 card: 前缀 meta 键。
func (s *Store) ListCards(ctx context.Context, batch string) ([]*Card, error) {
	if batch != "" {
		return s.listCardsByBatch(ctx, batch)
	}
	keys, err := s.scanKeys(ctx, keyCard)
	if err != nil {
		return nil, err
	}
	out := make([]*Card, 0, len(keys))
	for _, k := range keys {
		c, err := s.GetCardByHash(ctx, k[len(keyCard):])
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *Store) listCardsByBatch(ctx context.Context, batch string) ([]*Card, error) {
	fields, _, err := s.cli.WithContext(ctx).HScanPage(keyCardIdx+batch, 0, nil)
	if err != nil {
		return nil, err
	}
	out := make([]*Card, 0, len(fields))
	for _, f := range fields {
		c, err := s.GetCardByHash(ctx, f.Value.String())
		if errors.Is(err, ErrCardNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}
