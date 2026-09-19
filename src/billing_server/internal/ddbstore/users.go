package ddbstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"

	redimo "github.com/aura-studio/redimo/v2"
)

// User 管理员账号（余额在卡上，用户不再有 balance）。
type User struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Token     string `json:"token"`
	IsAdmin   bool   `json:"is_admin"`
	CreatedAt int64  `json:"created_at"`
}

func NewToken(prefix string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// EnsureAdmin 幂等保证名为 admin 的管理员账号存在且持有指定 token。
func (s *Store) EnsureAdmin(ctx context.Context, token string) error {
	u, err := s.GetUserByToken(ctx, token)
	if err == nil {
		if u.IsAdmin {
			return nil
		}
		_, err := s.casUser(ctx, u, func(n *User) { n.IsAdmin = true })
		return err
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	id, err := s.cli.WithContext(ctx).INCR(keySeq + "user")
	if err != nil {
		return err
	}
	u = &User{ID: id, Name: "admin", Token: token, IsAdmin: true, CreatedAt: s.Now().Unix()}
	if _, err := s.cli.WithContext(ctx).CreateTypeIfAbsent(keyUser+token, redimo.TypeString, 0, u.CreatedAt); err != nil {
		return err
	}
	ok, err := s.cli.WithContext(ctx).SET(keyUser+token, mustMarshal(u), redimo.IfNotExists)
	if err != nil {
		return err
	}
	if !ok {
		return ErrDuplicate
	}
	return nil
}

func (s *Store) GetUserByToken(ctx context.Context, token string) (*User, error) {
	rv, err := s.cli.WithContext(ctx).GET(keyUser + token)
	if err != nil {
		return nil, err
	}
	if rv.Empty() {
		return nil, ErrNotFound
	}
	var u User
	if err := json.Unmarshal([]byte(rv.String()), &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// casUser 整值 CAS 改写管理员账号，有限重试。
func (s *Store) casUser(ctx context.Context, u *User, mutate func(*User)) (bool, error) {
	for attempt := 0; attempt < 5; attempt++ {
		oldJSON := mustMarshal(u)
		next := *u
		mutate(&next)
		ok, err := s.cli.WithContext(ctx).SETCAS(keyUser+u.Token,
			redimo.StringValue{S: mustMarshal(&next)}, redimo.StringValue{S: oldJSON}, true)
		if err != nil {
			return false, err
		}
		if ok {
			*u = next
			return true, nil
		}
		fresh, err := s.GetUserByToken(ctx, u.Token)
		if err != nil {
			return false, err
		}
		*u = *fresh
	}
	return false, nil
}
