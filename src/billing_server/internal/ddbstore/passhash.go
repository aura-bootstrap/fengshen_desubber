package ddbstore

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// 管理员密码哈希:PBKDF2-HMAC-SHA256(标准库 crypto/pbkdf2,零外部依赖),
// 与 slicer core/crypto.go 同参数,编码互通。

const (
	pbkdf2Iter    = 210000          // 迭代次数(OWASP 量级;仅登录/改密时计算,会话期不重复)
	pbkdf2KeyLen  = 32              // 派生密钥字节数
	pbkdf2Prefix  = "pbkdf2-sha256" // pass_hash 编码前缀
	pbkdf2SaltLen = 16              // 每用户随机盐字节数
)

// NewPassEpoch 账号新建(含删后同名重建)的初始 pass_epoch:随机值而非恒 1。
// 会话令牌只锚定 (username, pass_epoch);硬删后重建若 epoch 归 1,
// 上一化身会话期内签发的旧令牌会静默复活(含已被 logout 吊销的),删号即不再等于吊销。
func NewPassEpoch() (int64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	// [2, 2^62):避开历史初值 1,恒正(DDB Number 直存)
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return int64(v>>2) + 2, nil
}

// HashPasswordNew 随机盐 + PBKDF2 派生,编码 "pbkdf2-sha256$<iter>$<salt_b64>$<hash_b64>"。
func HashPasswordNew(pw string) (string, error) {
	salt := make([]byte, pbkdf2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	dk, err := pbkdf2.Key(sha256.New, pw, salt, pbkdf2Iter, pbkdf2KeyLen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s$%d$%s$%s", pbkdf2Prefix, pbkdf2Iter,
		base64.StdEncoding.EncodeToString(salt), base64.StdEncoding.EncodeToString(dk)), nil
}

// VerifyPassword 解析编码串重算 PBKDF2,恒定时间比对。形态/迭代/盐非法即失败(不报错,返回 false)。
func VerifyPassword(pw, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != pbkdf2Prefix {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 {
		return false
	}
	salt, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil || len(want) == 0 {
		return false
	}
	dk, err := pbkdf2.Key(sha256.New, pw, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(dk, want) == 1
}
