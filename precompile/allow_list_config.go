// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompile

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

type AllowListConfig struct {
	BlockTimestamp *big.Int

	AllowListAdmins []common.Address
}

func (c *AllowListConfig) PrecompileAddress() common.Address { return common.Address{} }

func (c *AllowListConfig) Timestamp() *big.Int { return c.BlockTimestamp }

func (c *AllowListConfig) Configure(state PrecompileStateDB) {
	for _, adminAddr := range c.AllowListAdmins {
		state.SetState(createAddressKey(adminAddr), common.Hash{2}) // TODO move shared functionality into this directory/file
	}
}

// createAddressKey converts [address] into a [common.Hash] value that can be used for a storage slot key
func createAddressKey(address common.Address) common.Hash {
	hashBytes := make([]byte, common.HashLength)
	copy(hashBytes, address[:])
	return common.BytesToHash(hashBytes)
}
