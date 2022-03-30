// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompile

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"
)

var (
	_ StatefulPrecompileConfig = &TxAllowListConfig{}
	// Singleton StatefulPrecompiledContract for W/R access to the contract deployer allow list.
	TxAllowListPrecompile StatefulPrecompiledContract = createAllowListPrecompile(TxAllowListAddress)

	ErrSenderAddressNotAllowListed = errors.New("cannot issue transaction from non-allow listed address")
)

// TxAllowListConfig wraps [AllowListConfig] and uses it to implement the StatefulPrecompileConfig
// interface while adding in the contract deployer specific precompile address.
type TxAllowListConfig struct {
	AllowListConfig
}

// Address returns the address of the contract deployer allow list.
func (c *TxAllowListConfig) Address() common.Address {
	return TxAllowListAddress
}

// Configure configures [state] with the desired admins based on [c].
func (c *TxAllowListConfig) Configure(state StateDB) {
	c.AllowListConfig.Configure(state, TxAllowListAddress)
}

// Contract returns the singleton stateful precompiled contract to be used for the allow list.
func (c *TxAllowListConfig) Contract() StatefulPrecompiledContract {
	return TxAllowListPrecompile
}

// GetTxAllowListStatus returns the role of [address] for the contract deployer
// allow list.
func GetTxAllowListStatus(stateDB StateDB, address common.Address) AllowListRole {
	return getAllowListStatus(stateDB, TxAllowListAddress, address)
}

// SetTxAllowListStatus sets the permissions of [address] to [role] for the
// tx allow list.
// assumes [role] has already been verified as valid.
func SetAllowListStatus(stateDB StateDB, address common.Address, role AllowListRole) {
	setAllowListRole(stateDB, TxAllowListAddress, address, role)
}
