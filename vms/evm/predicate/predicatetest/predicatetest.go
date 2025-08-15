// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicatetest

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/vms/evm/predicate"
)

// NewAccessList constructs a types.AccessList from raw predicate bytes for a
// given precompile address.
func NewAccessList(address common.Address, b []byte) types.AccessList {
	return types.AccessList{
		{
			Address:     address,
			StorageKeys: New(b),
		},
	}
}

// New constructs a predicate from raw predicate bytes.
//
// It packs the predicate by appending [predicate.Delimiter] and zero-padding to
// a multiple of 32 bytes, then splits into [common.Hash]s.
func New(b []byte) predicate.Predicate {
	numUnpaddedChunks := len(b) / common.HashLength
	chunks := make([]common.Hash, numUnpaddedChunks+1)
	// Copy over chunks that don't require padding.
	for i := range chunks[:numUnpaddedChunks] {
		chunks[i] = common.Hash(b[common.HashLength*i:])
	}

	// Add the delimiter and required padding to the last chunk.
	copy(chunks[numUnpaddedChunks][:], b[common.HashLength*numUnpaddedChunks:])
	chunks[numUnpaddedChunks][len(b)%common.HashLength] = predicate.Delimiter
	return chunks
}
