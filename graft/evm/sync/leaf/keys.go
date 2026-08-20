// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package leaf

import (
	"bytes"

	"github.com/ava-labs/libevm/common"
)

// WithinRange reports whether key is at or before end. An empty end is unbounded.
func WithinRange(key, end []byte) bool {
	return len(end) == 0 || bytes.Compare(key, end) <= 0
}

// NextRangeKey returns the next range's start, one past k.
func NextRangeKey(k []byte) []byte {
	next := common.CopyBytes(k)
	IncrementBytes(next)
	return next
}

// IncrementBytes adds 1 to b in place. All-0xff wraps to all-zeros.
func IncrementBytes(b []byte) {
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] < 0xff {
			b[i]++
			return
		}
		b[i] = 0
	}
}
