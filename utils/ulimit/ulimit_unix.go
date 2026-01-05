// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build linux || netbsd || openbsd
// +build linux netbsd openbsd

package ulimit

import (
	"fmt"
	"syscall"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
)

const DefaultFDLimit = 32 * 1024

// Set attempts to bump the Rlimit which has a soft (Cur) and a hard (Max) value.
// The soft limit is what is used by the kernel to report EMFILE errors. The hard
// limit is a secondary limit which the process can be bumped to without additional
// privileges. Bumping the Max limit further would require superuser privileges.
// If the current Max is below our recommendation we will warn on start.
// see: http://0pointer.net/blog/file-descriptor-limits.html
func Set(limit uint64, log logging.Logger) error {
	var rLimit syscall.Rlimit
	err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		return fmt.Errorf("error getting rlimit: %w", err)
	}

	if limit > rLimit.Max {
		return fmt.Errorf("error fd-limit: (%d) greater than max: (%d)", limit, rLimit.Max)
	}

	rLimit.Cur = limit

	// set new limit
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
		return fmt.Errorf("error setting fd-limit: %w", err)
	}

	// verify limit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
		return fmt.Errorf("error getting rlimit: %w", err)
	}

	if rLimit.Cur < DefaultFDLimit {
		log.Warn("fd-limit is less than recommended and could result in reduced performance",
			zap.Uint64("limit", rLimit.Cur),
			zap.Uint64("recommendedLimit", DefaultFDLimit),
		)
	}

	return nil
}
