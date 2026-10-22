// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !firewood_bazel

package firewood

import "github.com/ava-labs/firewood-go-ethhash/ffi"

func withPersistenceGap(gap uint64) ffi.Option {
	return ffi.WithDeferredPersistenceCommitCount(gap)
}
