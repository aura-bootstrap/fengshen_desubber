package ddbstore

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	redimo "github.com/aura-studio/redimo/v2"
)

// 管理员账户体系(仿 slicer user.go):root 拥有所有功能管理权(账号+业务),
// admin 只管业务。单键 acct:<username> 整条账户 JSON(SETNX 建号/整值 CAS 改密改态)。
// 密码 PBKDF2(passhash.go),会话为 HMAC 签名无状态令牌(httpserver/session.go)。
// 与旧版 user:<token> 键族互不相交;旧 token 鉴权已废,残留行不影响新体系。

const (
	RoleRoot  = "root"
	RoleAdmin = "admin"

	UserActive   = "active"
	UserDisabled = "disabled"
)

// Account 管理员账户行。PassHash 标 json:"-" 永不下发管理端。
type Account struct {
	Username  string `json:"username"`
	Role      string `json:"role"`
	PassHash  string `json:"-"`
	Status    string `json:"status"`
	PassEpoch int64  `json:"pass_epoch"` // 改密/重置/登出即 bump,旧会话随之失效
	Version   int64  `json:"version"`    // 乐观锁版本(root 改账号时校验)
	CreatedAt int64  `json:"created_at"`
	CreatedBy string `json:"created_by,omitempty"`
	UpdatedAt int64  `json:"updated_at,omitempty"`
}

// accountStored 落库形态:pass_hash 只在存储 JSON 中出现。
type accountStored struct {
	Username  string `json:"username"`
	Role      string `json:"role"`
	PassHash  string `json:"pass_hash"`
	Status    string `json:"status"`
	PassEpoch int64  `json:"pass_epoch"`
	Version   int64  `json:"version"`
	CreatedAt int64  `json:"created_at"`
	CreatedBy string `json:"created_by,omitempty"`
	UpdatedAt int64  `json:"updated_at,omitempty"`
}

func (a *Account) stored() accountStored {
	return accountStored{
		Username: a.Username, Role: a.Role, PassHash: a.PassHash, Status: a.Status,
		PassEpoch: a.PassEpoch, Version: a.Version, CreatedAt: a.CreatedAt,
		CreatedBy: a.CreatedBy, UpdatedAt: a.UpdatedAt,
	}
}

func (a *Account) fromStored(s *accountStored) {
	*a = Account{
		Username: s.Username, Role: s.Role, PassHash: s.PassHash, Status: s.Status,
		PassEpoch: s.PassEpoch, Version: s.Version, CreatedAt: s.CreatedAt,
		CreatedBy: s.CreatedBy, UpdatedAt: s.UpdatedAt,
	}
}

// CreateAccount SETNX 建号;撞名 ok=false。
func (s *Store) CreateAccount(ctx context.Context, a *Account) (bool, error) {
	st := a.stored()
	ok, err := s.cli.WithContext(ctx).SET(keyAcct+a.Username, mustMarshal(&st), redimo.IfNotExists)
	return ok, err
}

// GetAccount 读账号;不存在返回 ErrNotFound。
func (s *Store) GetAccount(ctx context.Context, username string) (*Account, error) {
	rv, err := s.cli.WithContext(ctx).GET(keyAcct + username)
	if err != nil {
		return nil, err
	}
	if rv.Empty() {
		return nil, ErrNotFound
	}
	var st accountStored
	if err := json.Unmarshal([]byte(rv.String()), &st); err != nil {
		return nil, err
	}
	var a Account
	a.fromStored(&st)
	return &a, nil
}

// ListAccounts 全量管理员,按用户名升序。
func (s *Store) ListAccounts(ctx context.Context) ([]Account, error) {
	keys, err := s.scanKeys(ctx, keyAcct)
	if err != nil {
		return nil, err
	}
	out := make([]Account, 0, len(keys))
	for _, k := range keys {
		var st accountStored
		rv, err := s.cli.WithContext(ctx).GET(k)
		if err != nil {
			return nil, err
		}
		if rv.Empty() {
			continue
		}
		if err := json.Unmarshal([]byte(rv.String()), &st); err != nil {
			return nil, err
		}
		var a Account
		a.fromStored(&st)
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out, nil
}

// CASAccount 整值 CAS 改写账号,有限重试(与 casCard 同则)。
func (s *Store) CASAccount(ctx context.Context, a *Account, mutate func(*Account)) (bool, error) {
	for attempt := 0; attempt < 5; attempt++ {
		st := a.stored()
		oldJSON := mustMarshal(&st)
		next := *a
		mutate(&next)
		nextStored := next.stored()
		ok, err := s.cli.WithContext(ctx).SETCAS(keyAcct+a.Username,
			redimo.StringValue{S: mustMarshal(&nextStored)}, redimo.StringValue{S: oldJSON}, true)
		if err != nil {
			return false, err
		}
		if ok {
			*a = next
			return true, nil
		}
		fresh, err := s.GetAccount(ctx, a.Username)
		if err != nil {
			return false, err
		}
		*a = *fresh
	}
	return false, nil
}

// DeleteAccount 删除账户。redimo 无条件删原语,版本/角色校验已在调用方
// (httpserver.accountOp)完成;并发改动与删除互相覆盖的窗口对管理后台可接受。
func (s *Store) DeleteAccount(ctx context.Context, a *Account) (bool, error) {
	_, err := s.cli.WithContext(ctx).DEL(keyAcct + a.Username)
	if err != nil {
		return false, err
	}
	return true, nil
}

// EnsureRoot 部署期幂等种入超级管理员:root_password 非空且 root 不存在则建。
// 已存在则跳过(不覆盖密码,避免每次部署重置 root 口令)。恢复手段:删 root 行后重启。
func (s *Store) EnsureRoot(ctx context.Context, password string) error {
	if password == "" {
		return nil
	}
	_, err := s.GetAccount(ctx, RoleRoot)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	hash, err := HashPasswordNew(password)
	if err != nil {
		return err
	}
	epoch, err := NewPassEpoch()
	if err != nil {
		return err
	}
	now := s.Now().Unix()
	_, err = s.CreateAccount(ctx, &Account{
		Username: RoleRoot, Role: RoleRoot, PassHash: hash, Status: UserActive,
		PassEpoch: epoch, Version: 1, CreatedAt: now, CreatedBy: "system", UpdatedAt: now,
	})
	return err // 并发种入:撞名(ok=false)视为成功
}

// dummyPassHash 合法形态的哑哈希:登录时序均衡,账号不存在也跑一遍同参数 PBKDF2。
const dummyPassHash = "pbkdf2-sha256$210000$AAAAAAAAAAAAAAAAAAAAAA==$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

// CheckLogin 校验用户名+密码(status active 且 PBKDF2 匹配);
// 失败统一 false(不区分账号/密码,不泄露账号存在性)。爆破锁由调用方处理。
func (s *Store) CheckLogin(ctx context.Context, username, pw string) (*Account, bool, error) {
	a, err := s.GetAccount(ctx, username)
	if errors.Is(err, ErrNotFound) {
		VerifyPassword(pw, dummyPassHash)
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if a.Status != UserActive || !VerifyPassword(pw, a.PassHash) {
		return nil, false, nil
	}
	return a, true, nil
}

// ValidateUsername 建号规则(镜像 slicer):3..32 位,字母/数字/._-,root 保留。
func ValidateUsername(u string) string {
	if u == "" {
		return "用户名不能为空"
	}
	if len(u) < 3 || len(u) > 32 {
		return "用户名长度须 3..32"
	}
	if u == RoleRoot {
		return "该用户名保留"
	}
	for _, r := range u {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '_' || r == '-' || r == '.'
		if !ok {
			return "用户名仅允许字母、数字及 _ - ."
		}
	}
	return ""
}

// ValidatePassword 密码规则:8..128 位。
func ValidatePassword(pw string) string {
	if len(pw) < 8 {
		return "密码长度至少 8 位"
	}
	if len(pw) > 128 {
		return "密码过长"
	}
	return ""
}
