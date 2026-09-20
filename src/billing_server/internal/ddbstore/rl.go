package ddbstore

import (
	"context"
	"encoding/json"
	"errors"

	redimo "github.com/aura-studio/redimo/v2"
)

// HitRate 固定窗口计数：窗口序号编进键，INCR 原子自增，返回本窗口第 n 次。
// 窗口滚动=换键；首写补 meta + SetExpire(windowSec*2) 让旧窗键过期。
func (s *Store) HitRate(ctx context.Context, scope, id string, windowSec int64) (int64, error) {
	now := s.Now().Unix()
	key := keyRate + scope + ":" + id + ":" + itoa(now/windowSec)
	n, err := s.cli.WithContext(ctx).INCR(key)
	if err != nil {
		return 0, err
	}
	if n == 1 {
		if _, err := s.cli.WithContext(ctx).CreateTypeIfAbsent(key, redimo.TypeString, 0, now); err != nil {
			return n, err
		}
		if _, err := s.cli.WithContext(ctx).SetExpire(key, now+windowSec*2); err != nil {
			return n, err
		}
	}
	return n, nil
}

type rateFailStored struct {
	Fails       int64 `json:"fails"`
	LockedUntil int64 `json:"locked_until"`
}

// FailLock 记录一次失败；达 limit 则锁 lockSec 秒并清零计数，返回锁定秒数（0=未锁）。
func (s *Store) FailLock(ctx context.Context, scope, id string, limit, lockSec int64) (int64, error) {
	key := keyRateFail + scope + ":" + id
	for attempt := 0; attempt < 3; attempt++ {
		now := s.Now().Unix()
		var old rateFailStored
		exists := false
		var oldJSON string
		rv, err := s.cli.WithContext(ctx).GET(key)
		if err != nil {
			return 0, err
		}
		if !rv.Empty() {
			exists = true
			oldJSON = rv.String()
			if err := json.Unmarshal([]byte(oldJSON), &old); err != nil {
				return 0, err
			}
		}
		next := rateFailStored{Fails: old.Fails + 1, LockedUntil: old.LockedUntil}
		tripped := next.Fails >= limit
		if tripped {
			next.LockedUntil = now + lockSec
			next.Fails = 0
		}
		newJSON := mustMarshal(next)
		var ok bool
		if exists {
			ok, err = s.cli.WithContext(ctx).SETCAS(key, redimo.StringValue{S: newJSON}, redimo.StringValue{S: oldJSON}, true)
		} else {
			ok, err = s.cli.WithContext(ctx).SETCAS(key, redimo.StringValue{S: newJSON}, nil, false)
		}
		if err != nil {
			return 0, err
		}
		if !ok {
			continue
		}
		if tripped {
			return lockSec, nil
		}
		return 0, nil
	}
	return 0, errors.New("ratelimit: cas conflict")
}

// LockedFor 查询锁定剩余秒（0=未锁）。
func (s *Store) LockedFor(ctx context.Context, scope, id string) (int64, error) {
	rv, err := s.cli.WithContext(ctx).GET(keyRateFail + scope + ":" + id)
	if err != nil || rv.Empty() {
		return 0, err
	}
	var rf rateFailStored
	if err := json.Unmarshal([]byte(rv.String()), &rf); err != nil {
		return 0, err
	}
	if rem := rf.LockedUntil - s.Now().Unix(); rem > 0 {
		return rem, nil
	}
	return 0, nil
}

// ResetFail 成功路径清零失败计数。
func (s *Store) ResetFail(ctx context.Context, scope, id string) error {
	_, err := s.cli.WithContext(ctx).DEL(keyRateFail + scope + ":" + id)
	return err
}
