// This file was taken from:
// https://github.com/OpenBazaar/openbazaar-go/blob/master/core/ulimit_non_unix.go

// +build darwin linux netbsd openbsd

package ulimit

const (
	// DefaultFDLimit is the default recommended number of FDs to allocate.
	DefaultFDLimit uint64 = 1 << 15 // 32k
)
