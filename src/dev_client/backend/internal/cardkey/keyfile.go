package cardkey

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// CloudCredential cloudauth.json 内存形态:云端服务地址 + 管理员账号/密码。
// 只用于开发版登录云端内部任务通道;不含卡面,也不含余额。
//
// 落盘为整体 DPAPI 形态:
//
//	{"v":2, "blob": base64(DPAPI(innerJSON))}
//
// innerJSON 为本结构体字段全集——跨机/跨用户拷贝一律解不出。
type CloudCredential struct {
	Server   string `json:"server"`
	Username string `json:"username"`
	Password string `json:"password"`
	SavedAt  int64  `json:"saved_at"`
}

type diskV2 struct {
	V    int    `json:"v"`
	Blob string `json:"blob"`
}

var ErrNotConfigured = errors.New("未配置云端账号")
var ErrCredentialInvalid = errors.New("本地云端凭据无法读取")

// CredentialPath cloudauth.json 路径(exe 旁)。
func CredentialPath(exeDir string) string { return filepath.Join(exeDir, "cloudauth.json") }

// LoadCredential 读取 cloudauth.json;缺失返回 ErrNotConfigured,密文损坏返回 ErrCredentialInvalid。
func LoadCredential(exeDir string) (*CloudCredential, error) {
	data, err := os.ReadFile(CredentialPath(exeDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotConfigured
		}
		return nil, err
	}
	var env diskV2
	if err := json.Unmarshal(data, &env); err != nil || env.V != 2 || env.Blob == "" {
		return nil, fmt.Errorf("%w: 文件内容非法", ErrCredentialInvalid)
	}
	blob, err := base64.StdEncoding.DecodeString(env.Blob)
	if err != nil {
		return nil, fmt.Errorf("%w: 外壳密文非法", ErrCredentialInvalid)
	}
	pt, err := unprotect(blob)
	if err != nil {
		return nil, fmt.Errorf("%w: DPAPI 解密失败", ErrCredentialInvalid)
	}
	var cred CloudCredential
	if err := json.Unmarshal(pt, &cred); err != nil {
		return nil, fmt.Errorf("%w: 凭据记录非法", ErrCredentialInvalid)
	}
	return &cred, nil
}

// SaveCredential 写 cloudauth.json:内层 JSON 整体 DPAPI 后包外壳(0600)。
// 原子写:先写 .tmp 再 rename,避免崩溃/断电留下半写文件。
func SaveCredential(exeDir string, cred *CloudCredential) error {
	inner, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	blob, err := protect(inner)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(diskV2{V: 2, Blob: base64.StdEncoding.EncodeToString(blob)}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(CredentialPath(exeDir), data, 0o600)
}

// writeFileAtomic 先写同目录临时文件再原子替换目标。
// Windows 上 atomicReplace 用 MoveFileEx(REPLACE_EXISTING|WRITE_THROUGH),
// 旧凭据在新凭据 durable 之前不会被删除。
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = atomicReplace(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

// ClearCredential 删除 cloudauth.json(只删本机凭据,不影响远端管理员账号)。
func ClearCredential(exeDir string) error {
	os.Remove(CredentialPath(exeDir) + ".tmp")
	err := os.Remove(CredentialPath(exeDir))
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return err
}
