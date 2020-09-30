// This file was taken from:
// https://github.com/OpenBazaar/openbazaar-go/blob/master/core/ulimit_non_unix.go

// +build !darwin
// +build !linux
// +build !netbsd
// +build !openbsd

package ulimit

// Set is a no-op on non-unix systems
func Set(uint64) error { return nil }
