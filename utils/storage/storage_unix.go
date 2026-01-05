// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !windows && !openbsd
// +build !windows,!openbsd

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
	if stat.Blocks == 0 {
		return 0, 0, errZeroAvailableBytes
	}
	avail := stat.Bavail * uint64(stat.Bsize)
	percentage := stat.Bavail * 100 / stat.Blocks
	return avail, percentage, nil
}
