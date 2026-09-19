package cardkey

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// KeyFile cardkey.json 内存形态:计费服务地址 + 卡面明文(请求要用)
// + 激活时绑定的机器码 + 最近一次查到的余额(远端不可达时兜底显示)。
//
// 落盘为整体 DPAPI 形态(授权码/机器码明文落盘可被直接读取,故全包进密文):
//
//	{"v":2, "blob": base64(DPAPI(innerJSON))}
//
// innerJSON 为本结构体字段全集——跨机/跨用户拷贝一律解不出。
type KeyFile struct {
	Server      string `json:"server"`
	CardKey     string `json:"card_key"`
	MachineHash string `json:"machine_hash"`
	Credits     int    `json:"credits"` // 本地缓存余额(每次远端查询成功后刷新)
	ActivatedAt int64  `json:"activated_at"`
}

// diskV2 落盘外壳:整体 DPAPI blob。
type diskV2 struct {
	V    int    `json:"v"`
	Blob string `json:"blob"`
}

// ErrNotActivated 表示 cardkey.json 不存在(未激活);
// ErrCredentialInvalid 表示文件存在但无法解码/解密,调用方须提示用户且不得擅自删除。
var ErrNotActivated = errors.New("未激活")
var ErrCredentialInvalid = errors.New("本地授权数据无法读取")

// KeyPath cardkey.json 路径(exe 旁)。
func KeyPath(exeDir string) string { return filepath.Join(exeDir, "cardkey.json") }

// LoadKeyFile 读取 cardkey.json;缺失返回 ErrNotActivated,密文损坏返回 ErrCredentialInvalid。
func LoadKeyFile(exeDir string) (*KeyFile, error) {
	data, err := os.ReadFile(KeyPath(exeDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotActivated
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
	var kf KeyFile
	if err := json.Unmarshal(pt, &kf); err != nil {
		return nil, fmt.Errorf("%w: 授权记录非法", ErrCredentialInvalid)
	}
	return &kf, nil
}

// SaveKeyFile 写 cardkey.json:内层 JSON 整体 DPAPI 后包外壳(0600)。
// 原子写:先写 .tmp 再 rename,避免崩溃/断电留下半写文件
// (cardkey.json 是同机余额查询的唯一凭据,半写=只能重新激活)。
func SaveKeyFile(exeDir string, kf *KeyFile) error {
	inner, err := json.Marshal(kf)
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
	return writeFileAtomic(KeyPath(exeDir), data, 0o600)
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

// ClearKeyFile 删除 cardkey.json(用户主动解除激活;不解远端绑定)。
func ClearKeyFile(exeDir string) error {
	os.Remove(KeyPath(exeDir) + ".tmp") // 顺带清原子写残留
	err := os.Remove(KeyPath(exeDir))
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return err
}
