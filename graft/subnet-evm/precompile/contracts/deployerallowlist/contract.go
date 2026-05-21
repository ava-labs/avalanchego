// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package deployerallowlist

import (
	"github.com/MetalBlockchain/libevm/common"

	"github.com/MetalBlockchain/metalgo/graft/subnet-evm/precompile/allowlist"
	"github.com/MetalBlockchain/metalgo/graft/subnet-evm/precompile/contract"
)

// Singleton StatefulPrecompiledContract for W/R access to the contract deployer allow list.
var ContractDeployerAllowListPrecompile contract.StatefulPrecompiledContract = allowlist.CreateAllowListPrecompile(ContractAddress)

// GetContractDeployerAllowListStatus returns the role of [address] for the contract deployer
// allow list.
func GetContractDeployerAllowListStatus(stateDB contract.StateReader, address common.Address) allowlist.Role {
	return allowlist.GetAllowListStatus(stateDB, ContractAddress, address)
}

// SetContractDeployerAllowListStatus sets the permissions of [address] to [role] for the
// contract deployer allow list.
// assumes [role] has already been verified as valid.
func SetContractDeployerAllowListStatus(stateDB contract.StateDB, address common.Address, role allowlist.Role) {
	allowlist.SetAllowListRole(stateDB, ContractAddress, address, role)
}
