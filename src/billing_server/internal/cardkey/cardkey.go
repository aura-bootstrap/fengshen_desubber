// Package cardkey 卡号内核：卡面生成/归一化/哈希/脱敏，纯逻辑零 IO。
// 卡面与哈希算法与 shipinhao-transcode-tool 完全一致（alphabet/归一化/双哈希候选），
// 使 license.db 迁移来的卡在新服务上可直接用原卡面鉴权。
package cardkey

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
)

// Alphabet 31 字符去混淆（无 0/1/I/L/O），与 shipinhao 一致。
const Alphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// ErrNoPepper CARD_PEPPER 未配置（发卡/鉴权 fail-fast）。
var ErrNoPepper = errors.New("CARD_PEPPER 未配置")

// Generate 生成五组五位卡面，如 "A7K9P-QR3ST-…"（无偏采样，对齐 secrets.choice）。
func Generate() (string, error) {
	max := big.NewInt(int64(len(Alphabet)))
	groups := make([]string, 5)
	for g := range groups {
		b := make([]byte, 5)
		for i := range b {
			n, err := rand.Int(rand.Reader, max)
			if err != nil {
				return "", err
			}
			b[i] = Alphabet[n.Int64()]
		}
		groups[g] = string(b)
	}
	return strings.Join(groups, "-"), nil
}

// Normalize 归一化：去空白/横线、转大写（与 shipinhao normalize_card 一致）。
func Normalize(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, "-", "")
	code = strings.ReplaceAll(code, " ", "")
	return code
}

// HashV2 HMAC-SHA256(pepper, 归一化卡面) hex（新卡存储形态）。
func HashV2(pepper, code string) (string, error) {
	if pepper == "" {
		return "", ErrNoPepper
	}
	m := hmac.New(sha256.New, []byte(pepper))
	m.Write([]byte(Normalize(code)))
	return hex.EncodeToString(m.Sum(nil)), nil
}

// HashV1 裸 SHA256(归一化卡面) hex（shipinhao 旧卡兼容）。
func HashV1(code string) string {
	sum := sha256.Sum256([]byte(Normalize(code)))
	return hex.EncodeToString(sum[:])
}

// CandidateHashes 查找候选：pepper 非空先 v2，再 v1（与 shipinhao candidate_hashes 一致）。
func CandidateHashes(pepper, code string) ([]string, error) {
	var out []string
	if pepper != "" {
		h, err := HashV2(pepper, code)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return append(out, HashV1(code)), nil
}

// Mask 脱敏展示：保留首组与末字符，如 "ABCDE…Z"。
func Mask(code string) string {
	n := Normalize(code)
	if len(n) <= 6 {
		return n
	}
	return n[:5] + "…" + n[len(n)-1:]
}
