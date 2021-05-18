// +build windows

package storage

import (
	"errors"
	"syscall"
	"unsafe"
)

func osDiskStat(path string) (uint64, error) {
	h := syscall.MustLoadDLL("kernel32.dll")
	c := h.MustFindProc("GetDiskFreeSpaceExW")
	lpFreeBytesAvailable := int64(0)
	lpTotalNumberOfBytes := int64(0)
	lpTotalNumberOfFreeBytes := int64(0)
	u16p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	_, _, status := c.Call(uintptr(unsafe.Pointer(u16p)),
		uintptr(unsafe.Pointer(&lpFreeBytesAvailable)),
		uintptr(unsafe.Pointer(&lpTotalNumberOfBytes)),
		uintptr(unsafe.Pointer(&lpTotalNumberOfFreeBytes)))
	err = nil
	if status != syscall.Errno(0) {
		err = errors.New("nonzero return from win32 call for disk space")
	}
	if err != nil {
		return 0, err
	}
	return uint64(lpFreeBytesAvailable), nil
}
