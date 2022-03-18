// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build linux || netbsd || openbsd
// +build linux netbsd openbsd

package ulimit

import (
	"fmt"
	"syscall"

	"github.com/ava-labs/avalanchego/utils/logging"
)

const DefaultFDLimit = 32 * 1024

// Set attempts to bump the Rlimit which has a soft (Cur) and a hard (Max) value.
// The soft limit is what is used by the kernel to report EMFILE errors. The hard
// limit is a secondary limit which the process can be bumped to without additional
// privileges. Bumping the Max limit further would require superuser privileges.
// If the current Max is below our recommendation we will warn on start.
// see: http://0pointer.net/blog/file-descriptor-limits.html
func Set(max uint64, log logging.Logger) error {
	var rLimit syscall.Rlimit
	err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		return fmt.Errorf("error getting rlimit: %w", err)
	}

	if max > rLimit.Max {
		return fmt.Errorf("error fd-limit: (%d) greater than max: (%d)", max, rLimit.Max)
	}

	rLimit.Cur = max

	// set new limit
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
		return fmt.Errorf("error setting fd-limit: %w", err)
	}

	// verify limit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
		return fmt.Errorf("error getting rlimit: %w", err)
	}

	if rLimit.Cur < DefaultFDLimit {
		log.Warn("fd-limit: (%d) is less than recommended: (%d) and could result in reduced performance", rLimit.Cur, DefaultFDLimit)
	}

	return nil
}
