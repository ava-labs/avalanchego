// This file was taken from:
// https://github.com/OpenBazaar/openbazaar-go/blob/master/core/ulimit_non_unix.go

//go:build !darwin && !linux && !netbsd && !openbsd
// +build !darwin,!linux,!netbsd,!openbsd

package ulimit

// Set is a no-op on non-unix systems
func Set(uint64) error { return nil }
