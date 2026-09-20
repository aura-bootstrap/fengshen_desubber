package cardkey

import (
	"errors"
	"net"
	"runtime"
	"strings"
	"testing"
)

// TestKeyFileRoundtrip 保存/读取往返:DPAPI 仅 Windows 可用,其它平台跳过。
func TestKeyFileRoundtrip(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("DPAPI 仅支持 Windows")
	}
	dir := t.TempDir()
	if _, err := LoadKeyFile(dir); !errors.Is(err, ErrNotActivated) {
		t.Fatalf("空目录应返回 ErrNotActivated, got %v", err)
	}
	want := &KeyFile{
		Server: "https://billing.example.com", CardKey: "ABCDE-FGHIJ-KLMNO",
		MachineHash: strings.Repeat("A", 64), Credits: 88, ActivatedAt: 1700000000,
	}
	if err := SaveKeyFile(dir, want); err != nil {
		t.Fatalf("SaveKeyFile: %v", err)
	}
	got, err := LoadKeyFile(dir)
	if err != nil {
		t.Fatalf("LoadKeyFile: %v", err)
	}
	if *got != *want {
		t.Fatalf("往返不一致: got %+v, want %+v", got, want)
	}
	if err := ClearKeyFile(dir); err != nil {
		t.Fatalf("ClearKeyFile: %v", err)
	}
	if _, err := LoadKeyFile(dir); !errors.Is(err, ErrNotActivated) {
		t.Fatalf("清除后应返回 ErrNotActivated, got %v", err)
	}
}

// TestMachineIDDegraded 盘序列号取不到时降级为仅 MAC 哈希且置 degraded=true。
func TestMachineIDDegraded(t *testing.T) {
	oldIfs, oldDisk := interfacesFunc, diskSerialFunc
	defer func() { interfacesFunc, diskSerialFunc = oldIfs, oldDisk }()
	interfacesFunc = func() ([]net.Interface, error) {
		return []net.Interface{{
			Name: "以太网", HardwareAddr: net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01},
			Flags: net.FlagUp | net.FlagBroadcast,
		}}, nil
	}
	diskSerialFunc = func() (string, error) { return "", errors.New("无盘序列号") }
	hash, degraded, err := MachineID()
	if err != nil {
		t.Fatalf("MachineID: %v", err)
	}
	if !degraded || len(hash) != 64 {
		t.Fatalf("应降级且哈希 64 字符: degraded=%v hash=%q", degraded, hash)
	}
}

// TestFirstPhysicalMACSkipsVirtual 黑名单虚拟网卡不进机器码。
func TestFirstPhysicalMACSkipsVirtual(t *testing.T) {
	old := interfacesFunc
	defer func() { interfacesFunc = old }()
	interfacesFunc = func() ([]net.Interface, error) {
		return []net.Interface{
			{Name: "WireGuard Tunnel", HardwareAddr: net.HardwareAddr{0x00}, Flags: net.FlagUp},
			{Name: "以太网", HardwareAddr: net.HardwareAddr{0x0A, 0x01}, Flags: net.FlagUp},
			{Name: "Loopback Pseudo-Interface 1", HardwareAddr: nil, Flags: net.FlagUp | net.FlagLoopback},
		}, nil
	}
	mac, err := firstPhysicalMAC()
	if err != nil {
		t.Fatalf("firstPhysicalMAC: %v", err)
	}
	if mac != "0a01" {
		t.Fatalf("应选物理网卡 0a01, got %q", mac)
	}
}
