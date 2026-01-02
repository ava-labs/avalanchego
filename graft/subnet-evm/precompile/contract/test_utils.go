// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package contract

import (
	"fmt"

	"github.com/ava-labs/libevm/common"
)

// PackOrderedHashesWithSelector packs the function selector and ordered list of hashes into [dst]
// byte slice.
// assumes that [dst] has sufficient room for [functionSelector] and [hashes].
// Kept for testing backwards compatibility.
func PackOrderedHashesWithSelector(dst []byte, functionSelector []byte, hashes []common.Hash) error {
	copy(dst[:len(functionSelector)], functionSelector)
	return PackOrderedHashes(dst[len(functionSelector):], hashes)
}

// PackOrderedHashes packs the ordered list of [hashes] into the [dst] byte buffer.
// assumes that [dst] has sufficient space to pack [hashes] or else this function will panic.
// Kept for testing backwards compatibility.
func PackOrderedHashes(dst []byte, hashes []common.Hash) error {
	if len(dst) != len(hashes)*common.HashLength {
		return fmt.Errorf("destination byte buffer has insufficient length (%d) for %d hashes", len(dst), len(hashes))
	}

	var (
		start = 0
		end   = common.HashLength
	)
	for _, hash := range hashes {
		copy(dst[start:end], hash.Bytes())
		start += common.HashLength
		end += common.HashLength
	}
	return nil
}

// PackedHash returns packed the byte slice with common.HashLength from [packed]
// at the given [index].
// Assumes that [packed] is composed entirely of packed 32 byte segments.
// Kept for testing backwards compatibility.
func PackedHash(packed []byte, index int) []byte {
	start := common.HashLength * index
	end := start + common.HashLength
	return packed[start:end]
}
