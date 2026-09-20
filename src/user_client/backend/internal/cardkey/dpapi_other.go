//go:build !windows

package cardkey

import "errors"

// 非 Windows 构建约束:DPAPI 不可用(引擎只发 Windows 桌面版,跨平台编译只为测试纯逻辑)。

func protect([]byte) ([]byte, error)   { return nil, errors.New("DPAPI 仅支持 Windows") }
func unprotect([]byte) ([]byte, error) { return nil, errors.New("DPAPI 仅支持 Windows") }
