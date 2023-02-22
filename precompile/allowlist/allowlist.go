// (c) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package allowlist

import (
	"errors"
	"fmt"

	"github.com/ava-labs/subnet-evm/precompile/contract"
	"github.com/ava-labs/subnet-evm/vmerrs"
	"github.com/ethereum/go-ethereum/common"
)

// AllowList is an abstraction that allows other precompiles to manage
// which addresses can call the precompile by maintaining an allowlist
// in the storage trie. Each account may have one of the following roles:
// 1. NoRole - this is equivalent to common.Hash{} and deletes the key from the DB when set
// 2. EnabledRole - allowed to call the precompile
// 3. Admin - allowed to both modify the allowlist and call the precompile

const (
	SetAdminFuncKey      = "setAdmin"
	SetEnabledFuncKey    = "setEnabled"
	SetNoneFuncKey       = "setNone"
	ReadAllowListFuncKey = "readAllowList"

	ModifyAllowListGasCost = contract.WriteGasCostPerSlot
	ReadAllowListGasCost   = contract.ReadGasCostPerSlot

	allowListInputLen = common.HashLength
)

var (
	NoRole      = Role(common.BigToHash(common.Big0)) // NoRole - this is equivalent to common.Hash{} and deletes the key from the DB when set
	EnabledRole = Role(common.BigToHash(common.Big1)) // EnabledRole - allowed to call the precompile
	AdminRole   = Role(common.BigToHash(common.Big2)) // Admin - allowed to both modify the allowlist and call the precompile

	// AllowList function signatures
	setAdminSignature      = contract.CalculateFunctionSelector("setAdmin(address)")
	setEnabledSignature    = contract.CalculateFunctionSelector("setEnabled(address)")
	setNoneSignature       = contract.CalculateFunctionSelector("setNone(address)")
	readAllowListSignature = contract.CalculateFunctionSelector("readAllowList(address)")
	// Error returned when an invalid write is attempted
	ErrCannotModifyAllowList = errors.New("non-admin cannot modify allow list")
)

// GetAllowListStatus returns the allow list role of [address] for the precompile
// at [precompileAddr]
func GetAllowListStatus(state contract.StateDB, precompileAddr common.Address, address common.Address) Role {
	// Generate the state key for [address]
	addressKey := address.Hash()
	return Role(state.GetState(precompileAddr, addressKey))
}

// SetAllowListRole sets the permissions of [address] to [role] for the precompile
// at [precompileAddr].
// assumes [role] has already been verified as valid.
func SetAllowListRole(stateDB contract.StateDB, precompileAddr, address common.Address, role Role) {
	// Generate the state key for [address]
	addressKey := address.Hash()
	// Assign [role] to the address
	// This stores the [role] in the contract storage with address [precompileAddr]
	// and [addressKey] hash. It means that any reusage of the [addressKey] for different value
	// conflicts with the same slot [role] is stored.
	// Precompile implementations must use a different key than [addressKey]
	stateDB.SetState(precompileAddr, addressKey, common.Hash(role))
}

// PackModifyAllowList packs [address] and [role] into the appropriate arguments for modifying the allow list.
// Note: [role] is not packed in the input value returned, but is instead used as a selector for the function
// selector that should be encoded in the input.
func PackModifyAllowList(address common.Address, role Role) ([]byte, error) {
	// function selector (4 bytes) + hash for address
	input := make([]byte, 0, contract.SelectorLen+common.HashLength)

	switch role {
	case AdminRole:
		input = append(input, setAdminSignature...)
	case EnabledRole:
		input = append(input, setEnabledSignature...)
	case NoRole:
		input = append(input, setNoneSignature...)
	default:
		return nil, fmt.Errorf("cannot pack modify list input with invalid role: %s", role)
	}

	input = append(input, address.Hash().Bytes()...)
	return input, nil
}

// PackReadAllowList packs [address] into the input data to the read allow list function
func PackReadAllowList(address common.Address) []byte {
	input := make([]byte, 0, contract.SelectorLen+common.HashLength)
	input = append(input, readAllowListSignature...)
	input = append(input, address.Hash().Bytes()...)
	return input
}

// createAllowListRoleSetter returns an execution function for setting the allow list status of the input address argument to [role].
// This execution function is speciifc to [precompileAddr].
func createAllowListRoleSetter(precompileAddr common.Address, role Role) contract.RunStatefulPrecompileFunc {
	return func(evm contract.AccessibleState, callerAddr, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
		if remainingGas, err = contract.DeductGas(suppliedGas, ModifyAllowListGasCost); err != nil {
			return nil, 0, err
		}

		if len(input) != allowListInputLen {
			return nil, remainingGas, fmt.Errorf("invalid input length for modifying allow list: %d", len(input))
		}

		modifyAddress := common.BytesToAddress(input)

		if readOnly {
			return nil, remainingGas, vmerrs.ErrWriteProtection
		}

		stateDB := evm.GetStateDB()

		// Verify that the caller is an admin with permission to modify the allow list
		callerStatus := GetAllowListStatus(stateDB, precompileAddr, callerAddr)
		if !callerStatus.IsAdmin() {
			return nil, remainingGas, fmt.Errorf("%w: %s", ErrCannotModifyAllowList, callerAddr)
		}

		SetAllowListRole(stateDB, precompileAddr, modifyAddress, role)
		// Return an empty output and the remaining gas
		return []byte{}, remainingGas, nil
	}
}

// createReadAllowList returns an execution function that reads the allow list for the given [precompileAddr].
// The execution function parses the input into a single address and returns the 32 byte hash that specifies the
// designated role of that address
func createReadAllowList(precompileAddr common.Address) contract.RunStatefulPrecompileFunc {
	return func(evm contract.AccessibleState, callerAddr common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
		if remainingGas, err = contract.DeductGas(suppliedGas, ReadAllowListGasCost); err != nil {
			return nil, 0, err
		}

		if len(input) != allowListInputLen {
			return nil, remainingGas, fmt.Errorf("invalid input length for read allow list: %d", len(input))
		}

		readAddress := common.BytesToAddress(input)
		role := GetAllowListStatus(evm.GetStateDB(), precompileAddr, readAddress)
		roleBytes := common.Hash(role).Bytes()
		return roleBytes, remainingGas, nil
	}
}

// CreateAllowListPrecompile returns a StatefulPrecompiledContract with R/W control of an allow list at [precompileAddr]
func CreateAllowListPrecompile(precompileAddr common.Address) contract.StatefulPrecompiledContract {
	// Construct the contract with no fallback function.
	allowListFuncs := CreateAllowListFunctions(precompileAddr)
	contract, err := contract.NewStatefulPrecompileContract(nil, allowListFuncs)
	// TODO Change this to be returned as an error after refactoring this precompile
	// to use the new precompile template.
	if err != nil {
		panic(err)
	}
	return contract
}

func CreateAllowListFunctions(precompileAddr common.Address) []*contract.StatefulPrecompileFunction {
	setAdmin := contract.NewStatefulPrecompileFunction(setAdminSignature, createAllowListRoleSetter(precompileAddr, AdminRole))
	setEnabled := contract.NewStatefulPrecompileFunction(setEnabledSignature, createAllowListRoleSetter(precompileAddr, EnabledRole))
	setNone := contract.NewStatefulPrecompileFunction(setNoneSignature, createAllowListRoleSetter(precompileAddr, NoRole))
	read := contract.NewStatefulPrecompileFunction(readAllowListSignature, createReadAllowList(precompileAddr))

	return []*contract.StatefulPrecompileFunction{setAdmin, setEnabled, setNone, read}
}
