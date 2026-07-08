// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hashdb

import (
	"bytes"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// withinRange reports whether key is at or before end. An empty end is unbounded.
func withinRange(key, end []byte) bool {
	return len(end) == 0 || bytes.Compare(key, end) <= 0
}

// nextRangeKey returns the next range's start, one past k.
func nextRangeKey(k []byte) []byte {
	next := common.CopyBytes(k)
	incrementBytes(next)
	return next
}

// incrementBytes adds 1 to b in place. All-0xff wraps to all-zeros.
func incrementBytes(b []byte) {
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] < 0xff {
			b[i]++
			return
		}
		b[i] = 0
	}
}

// segmentRange returns the inclusive range of the i-th of numSegments prefix splits.
func segmentRange(i, numSegments int) (start, end []byte) {
	step := 0x10000 / numSegments
	return addPadding(uint16(i*step), 0x00), addPadding(uint16(i*step+step-1), 0xff)
}

// addPadding returns a 32-byte key: pos big-endian, then padding.
func addPadding(pos uint16, padding byte) []byte {
	packer := wrappers.Packer{Bytes: make([]byte, common.HashLength)}
	packer.PackShort(pos)
	packer.PackFixedBytes(bytes.Repeat([]byte{padding}, common.HashLength-wrappers.ShortLen))
	return packer.Bytes
}
