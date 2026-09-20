package ddbstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	redimo "github.com/aura-studio/redimo/v2"

	"fengshen-desubber/billing_server/internal/cardkey"
)

const (
	CardInactive = "inactive" // 新卡未激活（未绑定机器码）
	CardActive   = "active"   // 已激活（已绑定机器码）
	CardRevoked  = "revoked"  // 吊销
)

// Card 卡账户：卡面即凭证，余额在卡上（落库 JSON 字段序固定=SETCAS 基线前提）。
// MachineHash 空 = 未绑定机器；BoundAt 为首次绑定时间（unix 秒）。
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

// RechargeCard 充值（只能给卡加点）。余额 CAS 为提交点，流水/审计追加留痕。
func (s *Store) RechargeCard(ctx context.Context, cardID, amount int64, ref string) (int64, error) {
	c, err := s.GetCardByID(ctx, cardID)
	if err != nil {
		return 0, err
	}
	ok, err := s.casCard(ctx, c, func(n *Card) { n.Balance += amount })
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, errors.New("recharge: cas conflict")
	}
	_ = s.AppendTx(ctx, c.ID, ref, "grant", amount, c.Balance)
	_ = s.AppendAudit(ctx, AuditEntry{Actor: "admin", Action: "recharge", Target: c.CodeMasked, Detail: fmt.Sprintf("+%d ref=%s", amount, ref), OK: true})
	return c.Balance, nil
}

// DebitCard 扣费：卡须 active 且余额充足。成功返回新余额。
func (s *Store) DebitCard(ctx context.Context, c *Card, amount int64, kind, ref string) (int64, error) {
	if c.Status != CardActive {
		return c.Balance, ErrCardRevoked
	}
	if c.Balance < amount {
		return c.Balance, ErrInsufficientBalance
	}
	ok, err := s.casCard(ctx, c, func(n *Card) { n.Balance -= amount })
	if err != nil {
		return 0, err
	}
	if !ok {
		return c.Balance, ErrInsufficientBalance // 并发冲突按不足处理，调用方重读重试
	}
	_ = s.AppendTx(ctx, c.ID, ref, kind, amount, c.Balance)
	return c.Balance, nil
}

// RefundCard 退款（不要求 active：吊销卡仍须能收退款）。
func (s *Store) RefundCard(ctx context.Context, cardID, amount int64, ref string) (int64, error) {
	c, err := s.GetCardByID(ctx, cardID)
	if err != nil {
		return 0, err
	}
	ok, err := s.casCard(ctx, c, func(n *Card) { n.Balance += amount })
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, errors.New("refund: cas conflict")
	}
	_ = s.AppendTx(ctx, c.ID, ref, "refund", amount, c.Balance)
	return c.Balance, nil
}

// BindCard 激活绑定：inactive 卡 CAS 绑定机器码并转 active，写审计 bind。
// 调用方须已校验 c.Status == CardInactive；并发冲突返回错误由调用方重读重试。
func (s *Store) BindCard(ctx context.Context, c *Card, machineHash string) error {
	ok, err := s.casCard(ctx, c, func(n *Card) {
		n.Status = CardActive
		n.MachineHash = machineHash
		n.BoundAt = s.Now().Unix()
	})
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("bind: cas conflict")
	}
	_ = s.AppendAudit(ctx, AuditEntry{Actor: "card", Action: "bind", Target: c.CodeMasked, Detail: "machine=" + machineHash, OK: true})
	return nil
}

// UnbindCard 解绑机器码：清绑定信息并回到 inactive（revoked 卡拒绝），写审计 unbind。
// 已是未绑定的 inactive 卡幂等通过。
func (s *Store) UnbindCard(ctx context.Context, hash, actor string) error {
	c, err := s.GetCardByHash(ctx, hash)
	if err != nil {
		return err
	}
	if c.Status == CardRevoked {
		return ErrCardRevoked
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
// 恢复到非吊销态一律落 inactive：机器绑定随吊销失效，须重新激活。
func (s *Store) SetCardStatus(ctx context.Context, hash, status, actor string) error {
	c, err := s.GetCardByHash(ctx, hash)
	if err != nil {
		return err
	}
	if c.Status == status {
		return nil
	}
	ok, err := s.casCard(ctx, c, func(n *Card) {
		n.Status = status
		if status != CardActive {
			// 非 active 态不允许残留机器绑定
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
