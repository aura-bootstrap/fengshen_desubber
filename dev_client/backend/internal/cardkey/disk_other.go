//go:build !windows

package cardkey

import "errors"

var errNativeHardwareUnsupported = errors.New("非 Windows 平台不支持原生硬件采集")

// querySystemDiskSerial 非 Windows 平台无原生盘序列号采集
// (引擎只发 Windows 桌面版,跨平台编译只为测试机器码等纯逻辑);
// 取不到序列号时 MachineID 走降级路径(仅 MAC 哈希)。
func querySystemDiskSerial() (string, error) {
	return "", errNativeHardwareUnsupported
}
