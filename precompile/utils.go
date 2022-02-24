// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompile

import (
	"github.com/ethereum/go-ethereum/common"
)

// CreateAddressKey converts [address] into a [common.Hash] value to be used as a storage slot key
func CreateAddressKey(address common.Address) common.Hash {
	hashBytes := make([]byte, common.HashLength)
	copy(hashBytes, address[:])
	return common.BytesToHash(hashBytes)
}
