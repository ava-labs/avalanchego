//go:build !windows
// +build !windows

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package storage

import "syscall"

func OsDiskStat(storagePath string) (uint64, error) {
	var stat syscall.Statfs_t
	err := syscall.Statfs(storagePath, &stat)
	if err != nil {
		return 0, err
	}
	avail := stat.Bavail * uint64(stat.Bsize)
	return avail, nil
}
