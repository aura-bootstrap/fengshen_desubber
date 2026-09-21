package httpserver

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
)

// 无状态签名会话令牌:mini-JWT,HMAC-SHA256(仿 slicer core/crypto.go 会话段)。
// 令牌形态 "base64url(payload).base64url(sig)" —— 含 '.' 是它与卡面
// (Crockford base32,无点)的快速区分特征,auth 中间件据此分流。

// sessionPayload 令牌载荷:用户名 + 口令纪元(改密/重置/禁用/登出即 bump,
// 旧令牌随之失效) + 到期 Unix 秒。
type sessionPayload struct {
	U     string `json:"u"`
	Epoch int64  `json:"e"`
	Exp   int64  `json:"exp"`
}

// sessionTTL 会话有效期 12h(与 slicer IssueSession 一致)。
const sessionTTL = 12 * 3600

// signSession 签发 "base64url(payload).base64url(HMAC-SHA256(payload_b64, secret))"。
func signSession(secret []byte, user string, epoch, exp int64) (string, error) {
	pt, err := json.Marshal(sessionPayload{U: user, Epoch: epoch, Exp: exp})
	if err != nil {
		return "", err
	}
	p := base64.RawURLEncoding.EncodeToString(pt)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(p))
	return p + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// verifySession 验签 + 反序列化;签名不符或形态非法即 ok=false。
// 到期/口令纪元/账号状态由调用方结合 Store 与当前时间核对。
func verifySession(secret []byte, token string) (sessionPayload, bool) {
	i := strings.IndexByte(token, '.')
	if i <= 0 || i == len(token)-1 {
		return sessionPayload{}, false
	}
	p, sig := token[:i], token[i+1:]
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return sessionPayload{}, false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(p))
	if !hmac.Equal(got, mac.Sum(nil)) {
		return sessionPayload{}, false
	}
	pt, err := base64.RawURLEncoding.DecodeString(p)
	if err != nil {
		return sessionPayload{}, false
	}
	var pl sessionPayload
	if err := json.Unmarshal(pt, &pl); err != nil {
		return sessionPayload{}, false
	}
	return pl, true
}
