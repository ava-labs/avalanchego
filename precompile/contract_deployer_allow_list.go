// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompile

import "github.com/ethereum/go-ethereum/common"

var (
	_ StatefulPrecompileConfig = &ContractDeployerAllowListConfig{}
	// Singleton StatefulPrecompiledContract for W/R access to the contract deployer allow list.
	ContractDeployerAllowListPrecompile StatefulPrecompiledContract = createAllowListPrecompile(ContractDeployerAllowListAddress)
)

// ContractDeployerAllowListConfig wraps [AllowListConfig] and uses it to implement the StatefulPrecompileConfig
// interface while adding in the contract deployer specific precompile address.
type ContractDeployerAllowListConfig struct {
	AllowListConfig
}

// Address returns the address of the contract deployer allow list.
func (c *ContractDeployerAllowListConfig) Address() common.Address {
	return ContractDeployerAllowListAddress
}

// Configure configures [state] with the desired admins based on [c].
func (c *ContractDeployerAllowListConfig) Configure(state StateDB) {
	c.AllowListConfig.Configure(state, ContractDeployerAllowListAddress)
}

// Contract returns the singleton stateful precompiled contract to be used for the allow list.
func (c *ContractDeployerAllowListConfig) Contract() StatefulPrecompiledContract {
	return ContractDeployerAllowListPrecompile
}

// GetContractDeployerAllowListStatus returns the role of [address] for the contract deployer
// allow list.
func GetContractDeployerAllowListStatus(stateDB StateDB, address common.Address) AllowListRole {
	return getAllowListStatus(stateDB, ContractDeployerAllowListAddress, address)
}

// SetContractDeployerAllowListStatus sets the permissions of [address] to [role] for the
// contract deployer allow list.
// assumes [role] has already been verified as valid.
func SetContractDeployerAllowListStatus(stateDB StateDB, address common.Address, role AllowListRole) {
	setAllowListRole(stateDB, ContractDeployerAllowListAddress, address, role)
}
