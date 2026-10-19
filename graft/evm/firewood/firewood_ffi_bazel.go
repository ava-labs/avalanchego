//go:build firewood_bazel

package firewood

import ffi "github.com/ava-labs/firewood-go-ethhash/ffi"

func withPersistenceGap(gap uint64) ffi.Option {
	return ffi.WithMaxPersistenceGap(gap)
}
