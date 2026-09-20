//go:build windows

package cardkey

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

const (
	ioctlStorageQueryProperty = 0x002D1400
	ioctlVolumeGetDiskExtents = 0x00560000
	storageDeviceProperty     = 0
	propertyStandardQuery     = 0
)

// querySystemDiskSerial 取系统盘所在物理盘的序列号:系统卷 -> 首个物理 extent
// -> PhysicalDriveN -> IOCTL 直读序列号,全程原生调用,不起 shell。
func querySystemDiskSerial() (string, error) {
	drive := strings.TrimSpace(os.Getenv("SystemDrive"))
	if drive == "" {
		drive = "C:"
	}
	drive = strings.TrimRight(drive, `\/`)
	if !strings.HasSuffix(drive, ":") {
		return "", errors.New("SystemDrive 形态非法")
	}
	volumePath, err := windows.UTF16PtrFromString(`\\.\` + drive)
	if err != nil {
		return "", err
	}
	h, err := windows.CreateFile(volumePath, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return "", fmt.Errorf("打开系统卷失败: %v", err)
	}
	defer windows.CloseHandle(h)

	// VOLUME_DISK_EXTENTS 开头为 DWORD 数量,x64 下补四字节对齐,
	// 随后是 DISK_EXTENT.DiskNumber;机器码只需首个 extent。
	buf := make([]byte, 32)
	var returned uint32
	if err := windows.DeviceIoControl(h, ioctlVolumeGetDiskExtents, nil, 0, &buf[0], uint32(len(buf)), &returned, nil); err != nil {
		return "", fmt.Errorf("查询系统卷物理盘失败: %v", err)
	}
	if returned < 12 || binary.LittleEndian.Uint32(buf[0:4]) == 0 {
		return "", errors.New("系统卷没有物理盘 extent")
	}
	diskNumber := binary.LittleEndian.Uint32(buf[8:12])
	if diskNumber > 15 {
		return "", fmt.Errorf("系统盘编号超出支持范围: %d", diskNumber)
	}
	return queryPhysicalDiskSerial(int(diskNumber))
}

// queryPhysicalDiskSerial 对 \\.\PhysicalDriveN 发 IOCTL_STORAGE_QUERY_PROPERTY
// 直读设备描述符里的序列号字段。
func queryPhysicalDiskSerial(number int) (string, error) {
	path, _ := windows.UTF16PtrFromString(fmt.Sprintf(`\\.\PhysicalDrive%d`, number))
	h, err := windows.CreateFile(path, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	query := [12]byte{}
	binary.LittleEndian.PutUint32(query[0:4], storageDeviceProperty)
	binary.LittleEndian.PutUint32(query[4:8], propertyStandardQuery)
	buf := make([]byte, 4096)
	var returned uint32
	if err := windows.DeviceIoControl(h, ioctlStorageQueryProperty, &query[0], uint32(len(query)), &buf[0], uint32(len(buf)), &returned, nil); err != nil {
		return "", err
	}
	if returned < 28 {
		return "", errors.New("磁盘描述符过短")
	}
	offset := binary.LittleEndian.Uint32(buf[24:28])
	if offset == 0 || offset >= returned {
		return "", errors.New("磁盘序列号缺失")
	}
	end := offset
	for end < returned && buf[end] != 0 {
		end++
	}
	return strings.TrimSpace(string(buf[offset:end])), nil
}
