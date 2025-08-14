// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicatetest

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/vms/evm/predicate"
)

// NewAccessList constructs a types.AccessList from raw predicate bytes for a
// given precompile address. It packs the predicate by appending a 0xff delimiter
// and zero-padding to a multiple of 32 bytes, then splits into 32-byte chunks
// (common.Hash) as required by the access list storage keys.
func NewAccessList(address common.Address, predicateBytes []byte) types.AccessList {
	packed := new(predicateBytes)
	numHashes := len(packed) / common.HashLength
	storageKeys := make([]common.Hash, numHashes)
	for i := range storageKeys {
		start := i * common.HashLength
		copy(storageKeys[i][:], packed[start:])
	}

	return types.AccessList{
		{
			Address:     address,
			StorageKeys: storageKeys,
		},
	}
}

func new(b []byte) []byte {
	bytes := make([]byte, len(b)+1)
	copy(bytes, b)
	bytes[len(b)] = 0xff
	return common.RightPadBytes(bytes, predicate.RoundUpTo32(len(bytes)))
}
