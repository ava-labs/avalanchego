// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"bytes"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

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
