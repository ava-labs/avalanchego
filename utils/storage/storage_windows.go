// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build windows
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

var errNonzeroErrorCode = errors.New("nonzero return from win32 call for disk space")

func AvailableBytes(path string) (uint64, error) {
	h, err := syscall.LoadDLL(KERNEL32DLL)
	if err != nil {
		return 0, err
	}
	c, err := h.FindProc(GETDISKFREESPACEEXW)
	if err != nil {
		return 0, err
	}
	var (
		lpFreeBytesAvailable     int64
		lpTotalNumberOfBytes     int64
		lpTotalNumberOfFreeBytes int64
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
