// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompile

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// CreateAddressKey converts [address] into a [common.Hash] value to be used as a storage slot key
func CreateAddressKey(address common.Address) common.Hash {
	hashBytes := make([]byte, common.HashLength)
	copy(hashBytes, address[:])
	return common.BytesToHash(hashBytes)
}

// isForked returns whether a fork scheduled at block s is active at the given head block.
func isForked(s, head *big.Int) bool {
	if s == nil || head == nil {
		return false
	}
	return s.Cmp(head) <= 0
}
