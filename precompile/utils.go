// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompile

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// CalculateFunctionSelector returns the 4 byte function selector that results from [functionSignature]
// Ex. the function setBalance(addr address, balance uint256) should be passed in as the string:
// "setBalance(address,uint256)"
func CalculateFunctionSelector(functionSignature string) []byte {
	hash := crypto.Keccak256([]byte(functionSignature))
	return hash[:4]
}

// CreateAddressKey converts [address] into a [common.Hash] value to be used as a storage slot key
func CreateAddressKey(address common.Address) common.Hash {
	hashBytes := make([]byte, common.HashLength)
	copy(hashBytes, address[:])
	return common.BytesToHash(hashBytes)
}
