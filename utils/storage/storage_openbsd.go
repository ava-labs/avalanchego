// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build openbsd
// +build openbsd

package storage

import (
	"errors"
	"syscall"
)

var errZeroAvailableBytes = errors.New("available blocks is reported as 0")

func AvailableBytes(storagePath string) (uint64, uint64, error) {
	var stat syscall.Statfs_t
	err := syscall.Statfs(storagePath, &stat)
	if err != nil {
		return 0, 0, err
	}
	if stat.F_blocks == 0 {
		return 0, 0, errZeroAvailableBytes
	}
	avail := stat.F_bavail * uint64(stat.F_bsize)
	percentage := stat.F_bavail * 100 / stat.F_blocks
	return avail, percentage, nil
}
