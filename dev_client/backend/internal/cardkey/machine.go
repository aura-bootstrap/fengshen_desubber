// Package cardkey 卡密客户端基础:机器码采集(网卡 MAC + 系统盘物理序列号)
// 与 cardkey.json 密钥保管(DPAPI 整体加密落盘)。
// 诚实边界:本机同 Windows 用户上下文可调 DPAPI 解出卡密——只挡跨机/跨用户拷贝,
// 真实防护强度在服务端(卡密与机器码绑定,余额扣减全在云端)。
package cardkey

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"sort"
	"strings"
)

// 虚拟/VPN/远控网卡黑名单(匹配网卡 Name,大小写不敏感子串)。
// 这些适配器的 MAC 随安装/重启漂移,不能进机器码。
var virtualNICBlacklist = []string{
	"tap-windows", "tap-win32", "wintun", "loopback", "zerotier",
	"oray", "向日葵", "gameviewer", "todesk", "sunlogin",
	"virtualbox", "vmware", "hyper-v", "tailscale", "wireguard",
}

// 可注入点(测试注入假网卡表/假磁盘序列号)。
var (
	interfacesFunc = net.Interfaces
	diskSerialFunc = querySystemDiskSerial
)

// MachineID 采集机器码:物理网卡 MAC 按字节升序取首个 + 系统盘所在物理盘序列号,
// 拼合后 SHA-256(hex 64 字符,大写)。
// 降级:物理盘序列号取不到时仅按 MAC 取哈希并置 degraded=true(服务端按同样规则比对,
// 降级机器码与完整机器码不同——同一台机器两次采集方式须一致)。
func MachineID() (hash string, degraded bool, err error) {
	mac, err := firstPhysicalMAC()
	if err != nil {
		return "", false, err
	}
	serial, serr := diskSerialFunc()
	if serr != nil || strings.TrimSpace(serial) == "" {
		sum := sha256.Sum256([]byte(mac + "|degraded"))
		return strings.ToUpper(hex.EncodeToString(sum[:])), true, nil
	}
	sum := sha256.Sum256([]byte(mac + "|" + strings.TrimSpace(serial)))
	return strings.ToUpper(hex.EncodeToString(sum[:])), false, nil
}

// firstPhysicalMAC 枚举网卡:剔除回环/无 MAC/黑名单虚拟网卡后,
// 按 MAC 字节升序取首个(不按网卡名称序——名称随系统语言漂移)。
func firstPhysicalMAC() (string, error) {
	ifs, err := interfacesFunc()
	if err != nil {
		return "", fmt.Errorf("枚举网卡失败: %v", err)
	}
	var macs [][]byte
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagLoopback != 0 || len(ifc.HardwareAddr) == 0 {
			continue
		}
		desc := strings.ToLower(ifc.Name)
		blocked := false
		for _, kw := range virtualNICBlacklist {
			if strings.Contains(desc, strings.ToLower(kw)) {
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}
		mac := make([]byte, len(ifc.HardwareAddr))
		copy(mac, ifc.HardwareAddr)
		macs = append(macs, mac)
	}
	if len(macs) == 0 {
		return "", fmt.Errorf("未找到可用的物理网卡")
	}
	sort.Slice(macs, func(i, j int) bool {
		return string(macs[i]) < string(macs[j]) // 字节序比较
	})
	return hex.EncodeToString(macs[0]), nil
}
