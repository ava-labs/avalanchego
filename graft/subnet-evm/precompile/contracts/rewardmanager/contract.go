// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Code generated
// This file is a generated precompile contract with stubbed abstract functions.

package rewardmanager

import (
	_ "embed"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/graft/evm/constants"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contract"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/vm"
)

const (
	AllowFeeRecipientsGasCost      uint64 = contract.WriteGasCostPerSlot + allowlist.ReadAllowListGasCost // write 1 slot + read allow list
	AreFeeRecipientsAllowedGasCost uint64 = allowlist.ReadAllowListGasCost
	CurrentRewardAddressGasCost    uint64 = allowlist.ReadAllowListGasCost
	DisableRewardsGasCost          uint64 = contract.WriteGasCostPerSlot + allowlist.ReadAllowListGasCost // write 1 slot + read allow list
	SetRewardAddressGasCost        uint64 = contract.WriteGasCostPerSlot + allowlist.ReadAllowListGasCost // write 1 slot + read allow list
)

// Singleton StatefulPrecompiledContract and signatures.
var (
	ErrCannotAllowFeeRecipients      = errors.New("non-enabled cannot call allowFeeRecipients")
	ErrCannotAreFeeRecipientsAllowed = errors.New("non-enabled cannot call areFeeRecipientsAllowed")
	ErrCannotCurrentRewardAddress    = errors.New("non-enabled cannot call currentRewardAddress")
	ErrCannotDisableRewards          = errors.New("non-enabled cannot call disableRewards")
	ErrCannotSetRewardAddress        = errors.New("non-enabled cannot call setRewardAddress")

	ErrCannotEnableBothRewards = errors.New("cannot enable both fee recipients and reward address at the same time")
	ErrEmptyRewardAddress      = errors.New("reward address cannot be empty")
	ErrInvalidLen              = errors.New("invalid input length for setting reward address")

	// RewardManagerRawABI contains the raw ABI of RewardManager contract.
	//go:embed IRewardManager.abi
	RewardManagerRawABI string

	RewardManagerABI        = contract.ParseABI(RewardManagerRawABI)
	RewardManagerPrecompile = createRewardManagerPrecompile() // will be initialized by init function

	rewardAddressStorageKey        = common.Hash{'r', 'a', 's', 'k'}
	allowFeeRecipientsAddressValue = common.Hash{'a', 'f', 'r', 'a', 'v'}
)

// GetRewardManagerAllowListStatus returns the role of [address] for the RewardManager list.
func GetRewardManagerAllowListStatus(stateDB contract.StateDB, address common.Address) allowlist.Role {
	return allowlist.GetAllowListStatus(stateDB, ContractAddress, address)
}

// SetRewardManagerAllowListStatus sets the permissions of [address] to [role] for the
// RewardManager list. Assumes [role] has already been verified as valid.
func SetRewardManagerAllowListStatus(stateDB contract.StateDB, address common.Address, role allowlist.Role) {
	allowlist.SetAllowListRole(stateDB, ContractAddress, address, role)
}

// PackAllowFeeRecipients packs the function selector (first 4 func signature bytes).
// This function is mostly used for tests.
func PackAllowFeeRecipients() ([]byte, error) {
	return RewardManagerABI.Pack("allowFeeRecipients")
}

// EnableAllowFeeRecipients enables fee recipients.
func EnableAllowFeeRecipients(stateDB contract.StateDB) {
	stateDB.SetState(ContractAddress, rewardAddressStorageKey, allowFeeRecipientsAddressValue)
}

// DisableFeeRewards disables rewards and burns them by sending to Blackhole Address.
func DisableFeeRewards(stateDB contract.StateDB) {
	stateDB.SetState(ContractAddress, rewardAddressStorageKey, common.BytesToHash(constants.BlackholeAddr.Bytes()))
}

func allowFeeRecipients(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, AllowFeeRecipientsGasCost); err != nil {
		return nil, 0, err
	}
	if readOnly {
		return nil, remainingGas, vm.ErrWriteProtection
	}
	// no input provided for this function

	// Allow list is enabled and AllowFeeRecipients is a state-changer function.
	// This part of the code restricts the function to be called only by enabled/admin addresses in the allow list.
	// You can modify/delete this code if you don't want this function to be restricted by the allow list.
	stateDB := accessibleState.GetStateDB()
	// Verify that the caller is in the allow list and therefore has the right to call this function.
	callerStatus := allowlist.GetAllowListStatus(stateDB, ContractAddress, caller)
	if !callerStatus.IsEnabled() {
		return nil, remainingGas, fmt.Errorf("%w: %s", ErrCannotAllowFeeRecipients, caller)
	}
	// allow list code ends here.

	if accessibleState.GetRules().IsDurangoActivated() {
		if remainingGas, err = contract.DeductGas(remainingGas, FeeRecipientsAllowedEventGasCost); err != nil {
			return nil, 0, err
		}
		topics, data, err := PackFeeRecipientsAllowedEvent(caller)
		if err != nil {
			return nil, remainingGas, err
		}
		stateDB.AddLog(&types.Log{
			Address:     ContractAddress,
			Topics:      topics,
			Data:        data,
			BlockNumber: accessibleState.GetBlockContext().Number().Uint64(),
		})
	}
	EnableAllowFeeRecipients(stateDB)
	// Return the packed output and the remaining gas
	return []byte{}, remainingGas, nil
}

// PackAreFeeRecipientsAllowed packs the include selector (first 4 func signature bytes).
// This function is mostly used for tests.
func PackAreFeeRecipientsAllowed() ([]byte, error) {
	return RewardManagerABI.Pack("areFeeRecipientsAllowed")
}

// PackAreFeeRecipientsAllowedOutput attempts to pack given isAllowed of type bool
// to conform the ABI outputs.
func PackAreFeeRecipientsAllowedOutput(isAllowed bool) ([]byte, error) {
	return RewardManagerABI.PackOutput("areFeeRecipientsAllowed", isAllowed)
}

func areFeeRecipientsAllowed(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, AreFeeRecipientsAllowedGasCost); err != nil {
		return nil, 0, err
	}
	// no input provided for this function

	stateDB := accessibleState.GetStateDB()
	var output bool
	_, output = GetStoredRewardAddress(stateDB)

	packedOutput, err := PackAreFeeRecipientsAllowedOutput(output)
	if err != nil {
		return nil, remainingGas, err
	}

	// Return the packed output and the remaining gas
	return packedOutput, remainingGas, nil
}

// PackCurrentRewardAddress packs the include selector (first 4 func signature bytes).
// This function is mostly used for tests.
func PackCurrentRewardAddress() ([]byte, error) {
	return RewardManagerABI.Pack("currentRewardAddress")
}

// PackCurrentRewardAddressOutput attempts to pack given rewardAddress of type common.Address
// to conform the ABI outputs.
func PackCurrentRewardAddressOutput(rewardAddress common.Address) ([]byte, error) {
	return RewardManagerABI.PackOutput("currentRewardAddress", rewardAddress)
}

// GetStoredRewardAddress returns the current value of the address stored under rewardAddressStorageKey.
// Returns an empty address and true if allow fee recipients is enabled, otherwise returns current reward address and false.
func GetStoredRewardAddress(stateDB contract.StateReader) (common.Address, bool) {
	val := stateDB.GetState(ContractAddress, rewardAddressStorageKey)
	return common.BytesToAddress(val.Bytes()), val == allowFeeRecipientsAddressValue
}

// StoreRewardAddress stores the given [val] under rewardAddressStorageKey.
func StoreRewardAddress(stateDB contract.StateDB, val common.Address) {
	stateDB.SetState(ContractAddress, rewardAddressStorageKey, common.BytesToHash(val.Bytes()))
}

// PackSetRewardAddress packs [addr] of type common.Address into the appropriate arguments for setRewardAddress.
// the packed bytes include selector (first 4 func signature bytes).
// This function is mostly used for tests.
func PackSetRewardAddress(addr common.Address) ([]byte, error) {
	return RewardManagerABI.Pack("setRewardAddress", addr)
}

// UnpackSetRewardAddressInput attempts to unpack [input] into the common.Address type argument
// assumes that [input] does not include selector (omits first 4 func signature bytes)
// if [useStrictMode] is true, it will return an error if the length of [input] is not divisible by 32
func UnpackSetRewardAddressInput(input []byte, useStrictMode bool) (common.Address, error) {
	// Solidity does not always pack the input to the correct length, and allows
	// for extra padding bytes to be added to the end of the input. Therefore, we have removed
	// this check with Durango. We still need to keep this check for backwards compatibility.
	// However, as opposed to other precompiles, we only check that the length is divisible by 32,
	// since historical execution didn't enforce any particular length.
	if useStrictMode && len(input)%32 != 0 {
		return common.Address{}, fmt.Errorf("%w: %d", ErrInvalidLen, len(input))
	}
	var unpacked common.Address
	if err := RewardManagerABI.UnpackInputIntoInterface(&unpacked, "setRewardAddress", input); err != nil {
		return common.Address{}, err
	}
	return unpacked, nil
}

func setRewardAddress(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, SetRewardAddressGasCost); err != nil {
		return nil, 0, err
	}
	if readOnly {
		return nil, remainingGas, vm.ErrWriteProtection
	}
	rules := accessibleState.GetRules()
	// attempts to unpack [input] into the arguments to the SetRewardAddressInput.
	// Assumes that [input] does not include selector
	// do not use strict mode after Durango
	useStrictMode := !rules.IsDurangoActivated()
	rewardAddress, err := UnpackSetRewardAddressInput(input, useStrictMode)
	if err != nil {
		return nil, remainingGas, err
	}

	// Allow list is enabled and SetRewardAddress is a state-changer function.
	// This part of the code restricts the function to be called only by enabled/admin addresses in the allow list.
	// You can modify/delete this code if you don't want this function to be restricted by the allow list.
	stateDB := accessibleState.GetStateDB()
	// Verify that the caller is in the allow list and therefore has the right to call this function.
	callerStatus := allowlist.GetAllowListStatus(stateDB, ContractAddress, caller)
	if !callerStatus.IsEnabled() {
		return nil, remainingGas, fmt.Errorf("%w: %s", ErrCannotSetRewardAddress, caller)
	}
	// allow list code ends here.

	// if input is empty, return an error
	if rewardAddress == (common.Address{}) {
		return nil, remainingGas, ErrEmptyRewardAddress
	}

	// Add a log to be handled if this action is finalized.
	if rules.IsDurangoActivated() {
		if remainingGas, err = contract.DeductGas(remainingGas, RewardAddressChangedEventGasCost); err != nil {
			return nil, 0, err
		}
		oldRewardAddress, _ := GetStoredRewardAddress(stateDB)
		topics, data, err := PackRewardAddressChangedEvent(caller, oldRewardAddress, rewardAddress)
		if err != nil {
			return nil, remainingGas, err
		}
		stateDB.AddLog(&types.Log{
			Address:     ContractAddress,
			Topics:      topics,
			Data:        data,
			BlockNumber: accessibleState.GetBlockContext().Number().Uint64(),
		})
	}
	StoreRewardAddress(stateDB, rewardAddress)
	// Return the packed output and the remaining gas
	return []byte{}, remainingGas, nil
}

func currentRewardAddress(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, CurrentRewardAddressGasCost); err != nil {
		return nil, 0, err
	}

	// no input provided for this function
	stateDB := accessibleState.GetStateDB()
	output, _ := GetStoredRewardAddress(stateDB)
	packedOutput, err := PackCurrentRewardAddressOutput(output)
	if err != nil {
		return nil, remainingGas, err
	}

	// Return the packed output and the remaining gas
	return packedOutput, remainingGas, nil
}

// PackDisableRewards packs the include selector (first 4 func signature bytes).
// This function is mostly used for tests.
func PackDisableRewards() ([]byte, error) {
	return RewardManagerABI.Pack("disableRewards")
}

func disableRewards(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, DisableRewardsGasCost); err != nil {
		return nil, 0, err
	}
	if readOnly {
		return nil, remainingGas, vm.ErrWriteProtection
	}
	// no input provided for this function

	// Allow list is enabled and DisableRewards is a state-changer function.
	// This part of the code restricts the function to be called only by enabled/admin addresses in the allow list.
	// You can modify/delete this code if you don't want this function to be restricted by the allow list.
	stateDB := accessibleState.GetStateDB()
	// Verify that the caller is in the allow list and therefore has the right to call this function.
	callerStatus := allowlist.GetAllowListStatus(stateDB, ContractAddress, caller)
	if !callerStatus.IsEnabled() {
		return nil, remainingGas, fmt.Errorf("%w: %s", ErrCannotDisableRewards, caller)
	}
	// allow list code ends here.

	if accessibleState.GetRules().IsDurangoActivated() {
		if remainingGas, err = contract.DeductGas(remainingGas, RewardsDisabledEventGasCost); err != nil {
			return nil, 0, err
		}
		topics, data, err := PackRewardsDisabledEvent(caller)
		if err != nil {
			return nil, remainingGas, err
		}
		stateDB.AddLog(&types.Log{
			Address:     ContractAddress,
			Topics:      topics,
			Data:        data,
			BlockNumber: accessibleState.GetBlockContext().Number().Uint64(),
		})
	}
	DisableFeeRewards(stateDB)
	// Return the packed output and the remaining gas
	return []byte{}, remainingGas, nil
}

// createRewardManagerPrecompile returns a StatefulPrecompiledContract with getters and setters for the precompile.
// Access to the getters/setters is controlled by an allow list for [precompileAddr].
func createRewardManagerPrecompile() contract.StatefulPrecompiledContract {
	var functions []*contract.StatefulPrecompileFunction
	functions = append(functions, allowlist.CreateAllowListFunctions(ContractAddress)...)
	abiFunctionMap := map[string]contract.RunStatefulPrecompileFunc{
		"allowFeeRecipients":      allowFeeRecipients,
		"areFeeRecipientsAllowed": areFeeRecipientsAllowed,
		"currentRewardAddress":    currentRewardAddress,
		"disableRewards":          disableRewards,
		"setRewardAddress":        setRewardAddress,
	}

	for name, function := range abiFunctionMap {
		method, ok := RewardManagerABI.Methods[name]
		if !ok {
			panic(fmt.Errorf("given method (%s) does not exist in the ABI", name))
		}
		functions = append(functions, contract.NewStatefulPrecompileFunction(method.ID, function))
	}

	// Construct the contract with no fallback function.
	statefulContract, err := contract.NewStatefulPrecompileContract(nil, functions)
	if err != nil {
		panic(err)
	}
	return statefulContract
}
