// This file was taken from:
// https://github.com/OpenBazaar/openbazaar-go/blob/master/core/ulimit_non_unix.go

//go:build darwin || linux || netbsd || openbsd
// +build darwin linux netbsd openbsd

package ulimit

import (
	"fmt"
	"runtime"
	"syscall"
)

// Set the file descriptor limit
func Set(limit uint64) error {
	var rLimit syscall.Rlimit
	err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		return fmt.Errorf("error getting rlimit: %w", err)
	}

	oldMax := rLimit.Max
	if rLimit.Cur < limit {
		if rLimit.Max < limit {
			rLimit.Max = limit
		}
		rLimit.Cur = limit
	}

	// If we're on darwin, work around the fact that Getrlimit reports the wrong
	// value. See https://github.com/golang/go/issues/30401
	if runtime.GOOS == "darwin" && rLimit.Cur > 10240 {
		// The max file limit is 10240, even though the max returned by
		// Getrlimit is 1<<63-1. This is OPEN_MAX in sys/syslimits.h.
		rLimit.Max = 10240
		rLimit.Cur = 10240
	}

	// Try updating the limit. If it fails, try using the previous maximum
	// instead of our new maximum. Not all users have permissions to increase
	// the maximum.
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
		rLimit.Max = oldMax
		rLimit.Cur = oldMax
		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
			return fmt.Errorf("error setting ulimit: %w", err)
		}
	}

	return nil
}
