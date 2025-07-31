// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !windows && !openbsd
// +build !windows,!openbsd

package storage

import "syscall"

func AvailableBytes(storagePath string) (uint64, error) {
	var stat syscall.Statfs_t
	err := syscall.Statfs(storagePath, &stat)
	if err != nil {
		return 0, err
	}
	avail := stat.Bavail * uint64(stat.Bsize)
	return avail, nil
}
