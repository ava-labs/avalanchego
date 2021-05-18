// +build windows

package storage

import (
	"errors"
	"syscall"
	"unsafe"
)

const (
	KERNEL32DLL         = "kernel32.dll"
	GETDISKFREESPACEEXW = "GetDiskFreeSpaceExW"
)

func OsDiskStat(path string) (uint64, error) {
	h := syscall.MustLoadDLL(KERNEL32DLL)
	c := h.MustFindProc(GETDISKFREESPACEEXW)
	var (
		lpFreeBytesAvailable     int64
		lpTotalNumberOfBytes     int64
		lpTotalNumberOfFreeBytes int64
	)
	var (
		errNonzeroErrorCode = errors.new("nonzero return from win32 call for disk space")
	)
	u16p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	_, _, status := c.Call(uintptr(unsafe.Pointer(u16p)),
		uintptr(unsafe.Pointer(&lpFreeBytesAvailable)),
		uintptr(unsafe.Pointer(&lpTotalNumberOfBytes)),
		uintptr(unsafe.Pointer(&lpTotalNumberOfFreeBytes)))
	if status != syscall.Errno(0) {
		return 0, errNonzeroErrorCode
	}
	return uint64(lpFreeBytesAvailable), nil
}
