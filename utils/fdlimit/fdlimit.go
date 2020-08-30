// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// +build !windows

package fdlimit

import (
	"fmt"
	"syscall"
)

const (
	// DefaultFdLimit is the default value to raise the file
	// descriptor limit to on startup.
	DefaultFdLimit uint64 = 4096
)

// Based off of Quorum implementation:
// https://github.com/ConsenSys/quorum/tree/c215989c10f191924e6b5b668ba4ed8ed425ded1/common/fdlimit

// GetLimit returns the current and max file descriptor limit
func GetLimit() (uint64, uint64, error) {
	var limit syscall.Rlimit
	err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit)
	return limit.Cur, limit.Max, err
}

// RaiseLimit attempts to raise the file descriptor limit to at least [fdLimit]
func RaiseLimit(fdLimit uint64) error {
	// Get the current and maximum file descriptor limit
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
		return err
	}

	// Check if it is necessary or possible to raise the file descriptor limit
	if limit.Cur >= fdLimit {
		return nil
	}
	if limit.Max < fdLimit {
		return fmt.Errorf("Cannot raise fd limit to %d because it is above system maximum of %d", fdLimit, limit.Max)
	}

	// Attempt to increase the file descriptor limit to [fdLimit]
	limit.Cur = fdLimit
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
		return err
	}

	// Get the limit one last time to ensure that the OS made
	// the requested change
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
		return err
	}
	if limit.Cur < fdLimit {
		return fmt.Errorf("Raising the fd limit failed to take effect")
	}

	return nil
}
