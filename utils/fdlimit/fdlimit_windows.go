// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// This tag is applied for consistency with fdlimit.go, but
// is redundant because this file also uses *_windows.go suffix
// +build windows

package fdlimit

import (
	"fmt"
)

const (
	// DefaultFdLimit is the default value to raise the file
	// descriptor limit to on startup.
	DefaultFdLimit uint64 = 4096
	// Cannot adjust the fd limit set by the Windows OS
	// so these operations are NOPs
	windowsHardLimit uint64 = 16384
)

// Based off of Quorum implementation:
// https://github.com/ConsenSys/quorum/tree/c215989c10f191924e6b5b668ba4ed8ed425ded1/common/fdlimit

// GetLimit returns the current and max file descriptor limit
func GetLimit() (uint64, uint64, error) {
	return windowsHardLimit, windowsHardLimit, nil
}

// RaiseLimit attempts to raise the file descriptor limit to at least [fdLimit]
func RaiseLimit(fdLimit uint64) error {
	if fdLimit > windowsHardLimit {
		return fmt.Errorf("Cannot raise the file descriptor limit above %d to %d on Windows OS", windowsHardLimit, fdLimit)
	}
	return nil
}
