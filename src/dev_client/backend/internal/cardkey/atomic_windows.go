//go:build windows

package cardkey

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	moveFileReplaceExisting = 0x1
	moveFileWriteThrough    = 0x8
)

var moveFileExW = windows.NewLazySystemDLL("kernel32.dll").NewProc("MoveFileExW")

// atomicReplace Windows 下用 MoveFileEx(REPLACE_EXISTING|WRITE_THROUGH) 原子替换,
// 保证旧凭据在新凭据落盘前不会被删。
func atomicReplace(source, target string) error {
	src, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	dst, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	r, _, callErr := moveFileExW.Call(uintptr(unsafe.Pointer(src)), uintptr(unsafe.Pointer(dst)), moveFileReplaceExisting|moveFileWriteThrough)
	if r == 0 {
		return callErr
	}
	return nil
}
