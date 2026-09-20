//go:build windows

package cardkey

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DPAPI(CryptProtectData/CryptUnprotectData):cardkey.json 落盘加密。
// 密钥派生自当前 Windows 用户凭据——只挡跨机/跨用户拷贝,不挡本机同用户进程(诚实边界)。

var (
	crypt32       = windows.NewLazySystemDLL("crypt32.dll")
	procProtect   = crypt32.NewProc("CryptProtectData")
	procUnprotect = crypt32.NewProc("CryptUnprotectData")
	kernel32      = windows.NewLazySystemDLL("kernel32.dll")
	procLocalFree = kernel32.NewProc("LocalFree")
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

// protect DPAPI 加密(当前用户范围)。
func protect(plain []byte) ([]byte, error) {
	return cryptBlob(procProtect, plain)
}

// unprotect DPAPI 解密。
func unprotect(ciphertext []byte) ([]byte, error) {
	return cryptBlob(procUnprotect, ciphertext)
}

func cryptBlob(proc *windows.LazyProc, in []byte) ([]byte, error) {
	if len(in) == 0 {
		return nil, fmt.Errorf("DPAPI 输入为空")
	}
	inBlob := dataBlob{cbData: uint32(len(in)), pbData: &in[0]}
	var outBlob dataBlob
	r, _, err := proc.Call(
		uintptr(unsafe.Pointer(&inBlob)),
		0, // ppszDataDescr
		0, // pOptionalEntropy
		0, // pvReserved
		0, // pPromptStruct
		0, // dwFlags(本机+本用户)
		uintptr(unsafe.Pointer(&outBlob)),
	)
	if r == 0 {
		return nil, fmt.Errorf("DPAPI 调用失败: %v", err)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(outBlob.pbData)))
	out := make([]byte, outBlob.cbData)
	copy(out, unsafe.Slice(outBlob.pbData, outBlob.cbData))
	return out, nil
}
