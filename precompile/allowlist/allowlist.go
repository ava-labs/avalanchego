// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package allowlist

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"

	_ "embed"

	"github.com/ava-labs/subnet-evm/precompile/contract"
)

// AllowList is an abstraction that allows other precompiles to manage
// which addresses can call the precompile by maintaining an allowlist
// in the storage trie.

const (
	ModifyAllowListGasCost = contract.WriteGasCostPerSlot
	ReadAllowListGasCost   = contract.ReadGasCostPerSlot

	allowListInputLen = common.HashLength
)

var (
	// Error returned when an invalid write is attempted
	ErrCannotModifyAllowList = errors.New("cannot modify allow list")

	// AllowListRawABI contains the raw ABI of AllowList library interface.
	//go:embed allowlist.abi
	AllowListRawABI string

	AllowListABI = contract.ParseABI(AllowListRawABI)
)

// GetAllowListStatus returns the allow list role of [address] for the precompile
// at [precompileAddr]
func GetAllowListStatus(state contract.StateReader, precompileAddr common.Address, address common.Address) Role {
	// Generate the state key for [address]
	addressKey := common.BytesToHash(address.Bytes())
	return Role(state.GetState(precompileAddr, addressKey))
}

// SetAllowListRole sets the permissions of [address] to [role] for the precompile
// at [precompileAddr].
// assumes [role] has already been verified as valid.
func SetAllowListRole(stateDB contract.StateDB, precompileAddr, address common.Address, role Role) {
	// Generate the state key for [address]
	addressKey := common.BytesToHash(address.Bytes())
	// Assign [role] to the address
	// This stores the [role] in the contract storage with address [precompileAddr]
	// and [addressKey] hash. It means that any reusage of the [addressKey] for different value
	// conflicts with the same slot [role] is stored.
	// Precompile implementations must use a different key than [addressKey]
	stateDB.SetState(precompileAddr, addressKey, role.Hash())
}

func PackModifyAllowList(address common.Address, role Role) ([]byte, error) {
	funcName, err := role.GetSetterFunctionName()
	if err != nil {
		return nil, err
	}
	return AllowListABI.Pack(funcName, address)
}

func UnpackModifyAllowListInput(input []byte, r Role, useStrictMode bool) (common.Address, error) {
	if useStrictMode && len(input) != allowListInputLen {
		return common.Address{}, fmt.Errorf("invalid input length for modifying allow list: %d", len(input))
	}

	funcName, err := r.GetSetterFunctionName()
	if err != nil {
		return common.Address{}, err
	}
	var modifyAddress common.Address
	err = AllowListABI.UnpackInputIntoInterface(&modifyAddress, funcName, input, useStrictMode)
	return modifyAddress, err
}

// createAllowListRoleSetter returns an execution function for setting the allow list status of the input address argument to [role].
// This execution function is specific to [precompileAddr].
func createAllowListRoleSetter(precompileAddr common.Address, role Role) contract.RunStatefulPrecompileFunc {
	return func(evm contract.AccessibleState, callerAddr, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
		if remainingGas, err = contract.DeductGas(suppliedGas, ModifyAllowListGasCost); err != nil {
			return nil, 0, err
		}

		// do not use strict mode after Durango
		useStrictMode := !contract.IsDurangoActivated(evm)
		modifyAddress, err := UnpackModifyAllowListInput(input, role, useStrictMode)
		if err != nil {
			return nil, remainingGas, err
		}

		if readOnly {
			return nil, remainingGas, vm.ErrWriteProtection
		}

		stateDB := evm.GetStateDB()

		// Verify that the caller is an admin with permission to modify the allow list
		callerStatus := GetAllowListStatus(stateDB, precompileAddr, callerAddr)
		// Verify that the address we are trying to modify has a status that allows it to be modified
		modifyStatus := GetAllowListStatus(stateDB, precompileAddr, modifyAddress)
		if !callerStatus.CanModify(modifyStatus, role) {
			return nil, remainingGas, fmt.Errorf("%w: modify address: %s, from role: %s, to role: %s", ErrCannotModifyAllowList, callerAddr, modifyStatus, role)
		}
		if contract.IsDurangoActivated(evm) {
			if remainingGas, err = contract.DeductGas(remainingGas, AllowListEventGasCost); err != nil {
				return nil, 0, err
			}
			topics, data, err := PackRoleSetEvent(role, modifyAddress, callerAddr, modifyStatus)
			if err != nil {
				return nil, remainingGas, err
			}
			stateDB.AddLog(&types.Log{
				Address:     precompileAddr,
				Topics:      topics,
				Data:        data,
				BlockNumber: evm.GetBlockContext().Number().Uint64(),
			})
		}

		SetAllowListRole(stateDB, precompileAddr, modifyAddress, role)

		return []byte{}, remainingGas, nil
	}
}

// PackReadAllowList packs [address] into the input data to the read allow list function
func PackReadAllowList(address common.Address) ([]byte, error) {
	return AllowListABI.Pack("readAllowList", address)
}

func UnpackReadAllowListInput(input []byte, useStrictMode bool) (common.Address, error) {
	if useStrictMode && len(input) != allowListInputLen {
		return common.Address{}, fmt.Errorf("invalid input length for read allow list: %d", len(input))
	}

	var modifyAddress common.Address
	err := AllowListABI.UnpackInputIntoInterface(&modifyAddress, "readAllowList", input, useStrictMode)
	return modifyAddress, err
}

func PackReadAllowListOutput(roleNumber *big.Int) ([]byte, error) {
	return AllowListABI.PackOutput("readAllowList", roleNumber)
}

// createReadAllowList returns an execution function that reads the allow list for the given [precompileAddr].
// The execution function parses the input into a single address and returns the 32 byte hash that specifies the
// designated role of that address
func createReadAllowList(precompileAddr common.Address) contract.RunStatefulPrecompileFunc {
	return func(evm contract.AccessibleState, callerAddr common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
		if remainingGas, err = contract.DeductGas(suppliedGas, ReadAllowListGasCost); err != nil {
			return nil, 0, err
		}

		// We skip the fixed length check with Durango
		useStrictMode := !contract.IsDurangoActivated(evm)
		readAddress, err := UnpackReadAllowListInput(input, useStrictMode)
		if err != nil {
			return nil, remainingGas, err
		}

		role := GetAllowListStatus(evm.GetStateDB(), precompileAddr, readAddress)
		packedOutput, err := PackReadAllowListOutput(role.Big())
		if err != nil {
			return nil, remainingGas, err
		}
		return packedOutput, remainingGas, nil
	}
}

// CreateAllowListPrecompile returns a StatefulPrecompiledContract with R/W control of an allow list at [precompileAddr]
func CreateAllowListPrecompile(precompileAddr common.Address) contract.StatefulPrecompiledContract {
	// Construct the contract with no fallback function.
	allowListFuncs := CreateAllowListFunctions(precompileAddr)
	contract, err := contract.NewStatefulPrecompileContract(nil, allowListFuncs)
	if err != nil {
		panic(err)
	}
	return contract
}

func CreateAllowListFunctions(precompileAddr common.Address) []*contract.StatefulPrecompileFunction {
	var functions []*contract.StatefulPrecompileFunction

	for name, method := range AllowListABI.Methods {
		var fn *contract.StatefulPrecompileFunction
		if name == "readAllowList" {
			fn = contract.NewStatefulPrecompileFunction(method.ID, createReadAllowList(precompileAddr))
		} else if adminFnName, _ := AdminRole.GetSetterFunctionName(); name == adminFnName {
			fn = contract.NewStatefulPrecompileFunction(method.ID, createAllowListRoleSetter(precompileAddr, AdminRole))
		} else if enabledFnName, _ := EnabledRole.GetSetterFunctionName(); name == enabledFnName {
			fn = contract.NewStatefulPrecompileFunction(method.ID, createAllowListRoleSetter(precompileAddr, EnabledRole))
		} else if noRoleFnName, _ := NoRole.GetSetterFunctionName(); name == noRoleFnName {
			fn = contract.NewStatefulPrecompileFunction(method.ID, createAllowListRoleSetter(precompileAddr, NoRole))
		} else if managerFnName, _ := ManagerRole.GetSetterFunctionName(); name == managerFnName {
			fn = contract.NewStatefulPrecompileFunctionWithActivator(method.ID, createAllowListRoleSetter(precompileAddr, ManagerRole), contract.IsDurangoActivated)
		} else {
			panic("unexpected method name: " + name)
		}
		functions = append(functions, fn)
	}
	return functions
}
