// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompile

import (
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

var (
	_ StatefulPrecompileConfig = &TxAllowListConfig{}
	// Singleton StatefulPrecompiledContract for W/R access to the contract deployer allow list.
	TxAllowListPrecompile StatefulPrecompiledContract = createAllowListPrecompile(TxAllowListAddress)

	ErrSenderAddressNotAllowListed = errors.New("cannot issue transaction from non-allow listed address")
)

// TxAllowListConfig wraps [AllowListConfig] and uses it to implement the StatefulPrecompileConfig
// interface while adding in the TxAllowList specific precompile address.
type TxAllowListConfig struct {
	AllowListConfig
	UpgradeableConfig
}

// NewTxAllowListConfig returns a config for a network upgrade at [blockTimestamp] that enables
// TxAllowList with the given [admins] as members of the allowlist.
func NewTxAllowListConfig(blockTimestamp *big.Int, admins []common.Address) *TxAllowListConfig {
	return &TxAllowListConfig{
		AllowListConfig:   AllowListConfig{AllowListAdmins: admins},
		UpgradeableConfig: UpgradeableConfig{BlockTimestamp: blockTimestamp},
	}
}

// NewDisableTxAllowListConfig returns config for a network upgrade at [blockTimestamp]
// that disables TxAllowList.
func NewDisableTxAllowListConfig(blockTimestamp *big.Int) *TxAllowListConfig {
	return &TxAllowListConfig{
		UpgradeableConfig: UpgradeableConfig{
			BlockTimestamp: blockTimestamp,
			Disable:        true,
		},
	}
}

// Address returns the address of the contract deployer allow list.
func (c *TxAllowListConfig) Address() common.Address {
	return TxAllowListAddress
}

// Configure configures [state] with the desired admins based on [c].
func (c *TxAllowListConfig) Configure(_ ChainConfig, state StateDB, _ BlockContext) {
	c.AllowListConfig.Configure(state, TxAllowListAddress)
}

// Contract returns the singleton stateful precompiled contract to be used for the allow list.
func (c *TxAllowListConfig) Contract() StatefulPrecompiledContract {
	return TxAllowListPrecompile
}

// Equal returns true if [s] is a [*TxAllowListConfig] and it has been configured identical to [c].
func (c *TxAllowListConfig) Equal(s StatefulPrecompileConfig) bool {
	// typecast before comparison
	other, ok := (s).(*TxAllowListConfig)
	if !ok {
		return false
	}
	return c.UpgradeableConfig.Equal(&other.UpgradeableConfig) && c.AllowListConfig.Equal(&other.AllowListConfig)
}

// GetTxAllowListStatus returns the role of [address] for the contract deployer
// allow list.
func GetTxAllowListStatus(stateDB StateDB, address common.Address) AllowListRole {
	return getAllowListStatus(stateDB, TxAllowListAddress, address)
}

// SetTxAllowListStatus sets the permissions of [address] to [role] for the
// tx allow list.
// assumes [role] has already been verified as valid.
func SetTxAllowListStatus(stateDB StateDB, address common.Address, role AllowListRole) {
	setAllowListRole(stateDB, TxAllowListAddress, address, role)
}
