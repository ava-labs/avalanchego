// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build freebsd
// +build freebsd

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
// If the value is below the recommendation warn on start.
// see: http://0pointer.net/blog/file-descriptor-limits.html
func Set(limit uint64, log logging.Logger) error {
	// Note: BSD Rlimit is type int64
	// ref: https://cs.opensource.google/go/x/sys/+/b874c991:unix/ztypes_freebsd_amd64.go
	bsdMax := int64(limit)
	var rLimit syscall.Rlimit
	err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		return fmt.Errorf("error getting rlimit: %w", err)
	}

	if bsdMax > rLimit.Max {
		return fmt.Errorf("error fd-limit: (%d) greater than max: (%d)", limit, rLimit.Max)
	}

	rLimit.Cur = bsdMax

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
			zap.Uint64("currentLimit", rLimit.Cur),
			zap.Uint64("recommendedLimit", DefaultFDLimit),
		)
	}

	return nil
}
