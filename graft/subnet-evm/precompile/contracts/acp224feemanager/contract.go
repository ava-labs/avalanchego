// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp224feemanager

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"

	_ "embed"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contract"
)

const (
	// Gas costs for each function.
	numFeeConfigField = 4 // targetGas, minGasPrice, maxCapacityFactor, timeToDouble
	// GetFeeConfigGasCost is the cost to read all fee config fields
	GetFeeConfigGasCost uint64 = contract.ReadGasCostPerSlot * numFeeConfigField
	// GetFeeConfigLastChangedAtGasCost is the cost to read the last changed at block number
	GetFeeConfigLastChangedAtGasCost uint64 = contract.ReadGasCostPerSlot
	// FeeConfigUpdatedEventGasCost is the cost to emit the FeeConfigUpdated event
	FeeConfigUpdatedEventGasCost uint64 = contract.LogGas + contract.LogTopicGas*2 + contract.LogDataGas*numFeeConfigField*common.HashLength*2
	// SetFeeConfigGasCost includes: allowlist read, storage writes, reading old config, and event emission
	SetFeeConfigGasCost uint64 = allowlist.ReadAllowListGasCost +
		contract.WriteGasCostPerSlot*(numFeeConfigField+1) + // plus one for setting last changed at
		GetFeeConfigGasCost + // for reading existing fee config to be emitted in event
		FeeConfigUpdatedEventGasCost
)

var (
	// Singleton StatefulPrecompiledContract for setting fee configs by permissioned callers.
	ACP224FeeManagerPrecompile contract.StatefulPrecompiledContract = createACP224FeeManagerPrecompile()

	// Storage layout uses namespaced keys to prevent collisions with allowlist and other precompile state.
	// AllowList uses keccak256(address) for role storage, so we use distinct prefixes for fee config.
	targetGasStorageKey         = common.Hash{'a', 'c', 'p', '2', '2', '4', 't', 'g'}
	minGasPriceStorageKey       = common.Hash{'a', 'c', 'p', '2', '2', '4', 'm', 'p'}
	maxCapacityFactorStorageKey = common.Hash{'a', 'c', 'p', '2', '2', '4', 'm', 'c'}
	timeToDoubleStorageKey      = common.Hash{'a', 'c', 'p', '2', '2', '4', 't', 'd'}
	feeConfigLastChangedAtKey   = common.Hash{'a', 'c', 'p', '2', '2', '4', 'l', 'c', 'a'}

	ErrCannotSetFeeConfig = errors.New("non-enabled cannot call setFeeConfig")

	// IACP224FeeManagerRawABI contains the raw ABI of ACP224FeeManager contract.
	//go:embed IACP224FeeManager.abi
	ACP224FeeManagerRawABI string

	ACP224FeeManagerABI = contract.ParseABI(ACP224FeeManagerRawABI)
)

// GetACP224FeeManagerAllowListStatus returns the role of [address] for the ACP224FeeManager list.
func GetACP224FeeManagerAllowListStatus(
	stateDB contract.StateDB,
	precompileAddr common.Address,
	address common.Address,
) allowlist.Role {
	return allowlist.GetAllowListStatus(stateDB, precompileAddr, address)
}

// SetACP224FeeManagerAllowListStatus sets the permissions of [address] to [role] for the
// ACP224FeeManager list. Assumes [role] has already been verified as valid.
// This stores the [role] in the contract storage with address [precompileAddr]
// and [address] hash. It means that any reusage of the [address] key for different value
// conflicts with the same slot [role] is stored.
// Precompile implementations must use a different key than [address] for their storage.
func SetACP224FeeManagerAllowListStatus(
	stateDB contract.StateDB,
	precompileAddr common.Address,
	address common.Address,
	role allowlist.Role,
) {
	allowlist.SetAllowListRole(stateDB, precompileAddr, address, role)
}

// GetStoredFeeConfig returns fee config from contract storage in given state
func GetStoredFeeConfig(stateDB contract.StateReader, addr common.Address) commontype.ACP224FeeConfig {
	return commontype.ACP224FeeConfig{
		TargetGas:         stateDB.GetState(addr, targetGasStorageKey).Big(),
		MinGasPrice:       stateDB.GetState(addr, minGasPriceStorageKey).Big(),
		MaxCapacityFactor: stateDB.GetState(addr, maxCapacityFactorStorageKey).Big(),
		TimeToDouble:      stateDB.GetState(addr, timeToDoubleStorageKey).Big(),
	}
}

// GetFeeConfigLastChangedAt returns the block number when the fee config was last changed
func GetFeeConfigLastChangedAt(stateDB contract.StateReader, addr common.Address) *big.Int {
	val := stateDB.GetState(addr, feeConfigLastChangedAtKey)
	return val.Big()
}

// StoreFeeConfig stores given [feeConfig] and block number in the [blockContext] to the [stateDB].
// A validation on [feeConfig] is done before storing.
func StoreFeeConfig(stateDB contract.StateDB, addr common.Address, feeConfig commontype.ACP224FeeConfig, blockContext contract.ConfigurationBlockContext) error {
	if err := feeConfig.Verify(); err != nil {
		return fmt.Errorf("cannot verify fee config: %w", err)
	}

	stateDB.SetState(addr, targetGasStorageKey, common.BigToHash(feeConfig.TargetGas))
	stateDB.SetState(addr, minGasPriceStorageKey, common.BigToHash(feeConfig.MinGasPrice))
	stateDB.SetState(addr, maxCapacityFactorStorageKey, common.BigToHash(feeConfig.MaxCapacityFactor))
	stateDB.SetState(addr, timeToDoubleStorageKey, common.BigToHash(feeConfig.TimeToDouble))

	blockNumber := blockContext.Number()
	if blockNumber == nil {
		return errors.New("blockNumber cannot be nil")
	}
	stateDB.SetState(addr, feeConfigLastChangedAtKey, common.BigToHash(blockNumber))
	return nil
}

// PackSetFeeConfig packs [config] of type commontype.ACP224FeeConfig into the appropriate arguments for setFeeConfig.
// the packed bytes include selector (first 4 func signature bytes).
// This function is mostly used for tests.
func PackSetFeeConfig(config commontype.ACP224FeeConfig) ([]byte, error) {
	return ACP224FeeManagerABI.Pack("setFeeConfig", config)
}

// UnpackSetFeeConfigInput attempts to unpack [input] into the commontype.ACP224FeeConfig type argument
// assumes that [input] does not include selector (omits first 4 func signature bytes)
func UnpackSetFeeConfigInput(input []byte) (commontype.ACP224FeeConfig, error) {
	res, err := ACP224FeeManagerABI.UnpackInput("setFeeConfig", input, false)
	if err != nil {
		return commontype.ACP224FeeConfig{}, err
	}
	unpacked := *abi.ConvertType(res[0], new(commontype.ACP224FeeConfig)).(*commontype.ACP224FeeConfig)
	return unpacked, nil
}

func setFeeConfig(
	accessibleState contract.AccessibleState,
	caller common.Address,
	addr common.Address,
	input []byte,
	suppliedGas uint64,
	readOnly bool,
) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, SetFeeConfigGasCost); err != nil {
		return nil, 0, err
	}

	if readOnly {
		return nil, remainingGas, vm.ErrWriteProtection
	}

	feeConfig, err := UnpackSetFeeConfigInput(input)
	if err != nil {
		return nil, remainingGas, err
	}

	stateDB := accessibleState.GetStateDB()
	// Verify that the caller is in the allow list and therefore has the right to call this function.
	callerStatus := GetACP224FeeManagerAllowListStatus(stateDB, addr, caller)
	if !callerStatus.IsEnabled() {
		return nil, remainingGas, fmt.Errorf("%w: %s", ErrCannotSetFeeConfig, caller)
	}

	oldConfig := GetStoredFeeConfig(stateDB, addr)
	topics, data, err := PackFeeConfigUpdatedEvent(
		caller,
		oldConfig,
		feeConfig,
	)
	if err != nil {
		return nil, remainingGas, err
	}

	stateDB.AddLog(&types.Log{
		Address:     addr,
		Topics:      topics,
		Data:        data,
		BlockNumber: accessibleState.GetBlockContext().Number().Uint64(),
	})

	if err := StoreFeeConfig(stateDB, addr, feeConfig, accessibleState.GetBlockContext()); err != nil {
		return nil, remainingGas, err
	}

	// Return an empty output and the remaining gas
	return []byte{}, remainingGas, nil
}

// PackGetFeeConfig packs the include selector (first 4 func signature bytes).
// This function is mostly used for tests.
func PackGetFeeConfig() ([]byte, error) {
	return ACP224FeeManagerABI.Pack("getFeeConfig")
}

// PackGetFeeConfigOutput attempts to pack given config of type commontype.ACP224FeeConfig
// to conform the ABI outputs.
func PackGetFeeConfigOutput(config commontype.ACP224FeeConfig) ([]byte, error) {
	return ACP224FeeManagerABI.PackOutput("getFeeConfig", config)
}

// UnpackGetFeeConfigOutput attempts to unpack given [output] into the commontype.ACP224FeeConfig type output
// assumes that [output] does not include selector (omits first 4 func signature bytes)
func UnpackGetFeeConfigOutput(output []byte) (commontype.ACP224FeeConfig, error) {
	res, err := ACP224FeeManagerABI.Unpack("getFeeConfig", output)
	if err != nil {
		return commontype.ACP224FeeConfig{}, err
	}
	unpacked := *abi.ConvertType(res[0], new(commontype.ACP224FeeConfig)).(*commontype.ACP224FeeConfig)
	return unpacked, nil
}

func getFeeConfig(
	accessibleState contract.AccessibleState,
	_ common.Address,
	addr common.Address,
	_ []byte,
	suppliedGas uint64,
	_ bool,
) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, GetFeeConfigGasCost); err != nil {
		return nil, 0, err
	}

	feeConfig := GetStoredFeeConfig(accessibleState.GetStateDB(), addr)

	output, err := PackGetFeeConfigOutput(feeConfig)
	if err != nil {
		return nil, remainingGas, err
	}

	// Return the fee config as output and the remaining gas
	return output, remainingGas, err
}

// PackGetFeeConfigLastChangedAt packs the include selector (first 4 func signature bytes).
// This function is mostly used for tests.
func PackGetFeeConfigLastChangedAt() ([]byte, error) {
	return ACP224FeeManagerABI.Pack("getFeeConfigLastChangedAt")
}

// PackGetFeeConfigLastChangedAtOutput attempts to pack given blockNumber of type *big.Int
// to conform the ABI outputs.
func PackGetFeeConfigLastChangedAtOutput(blockNumber *big.Int) ([]byte, error) {
	return ACP224FeeManagerABI.PackOutput("getFeeConfigLastChangedAt", blockNumber)
}

// UnpackGetFeeConfigLastChangedAtOutput attempts to unpack given [output] into the *big.Int type output
// assumes that [output] does not include selector (omits first 4 func signature bytes)
func UnpackGetFeeConfigLastChangedAtOutput(output []byte) (*big.Int, error) {
	res, err := ACP224FeeManagerABI.Unpack("getFeeConfigLastChangedAt", output)
	if err != nil {
		return new(big.Int), err
	}
	unpacked := *abi.ConvertType(res[0], new(*big.Int)).(**big.Int)
	return unpacked, nil
}

func getFeeConfigLastChangedAt(
	accessibleState contract.AccessibleState,
	_ common.Address,
	addr common.Address,
	_ []byte,
	suppliedGas uint64,
	_ bool,
) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, GetFeeConfigLastChangedAtGasCost); err != nil {
		return nil, 0, err
	}

	lastChangedAt := GetFeeConfigLastChangedAt(accessibleState.GetStateDB(), addr)
	packedOutput, err := PackGetFeeConfigLastChangedAtOutput(lastChangedAt)
	if err != nil {
		return nil, remainingGas, err
	}

	return packedOutput, remainingGas, err
}

// createACP224FeeManagerPrecompile returns a StatefulPrecompiledContract with getters and setters for the precompile.
// Access to the getters/setters is controlled by an allow list for ContractAddress.
func createACP224FeeManagerPrecompile() contract.StatefulPrecompiledContract {
	abiFunctionMap := map[string]contract.RunStatefulPrecompileFunc{
		"getFeeConfig":              getFeeConfig,
		"getFeeConfigLastChangedAt": getFeeConfigLastChangedAt,
		"setFeeConfig":              setFeeConfig,
	}
	functions := make([]*contract.StatefulPrecompileFunction, 0, len(abiFunctionMap)+len(allowlist.AllowListABI.Methods))
	functions = append(functions, allowlist.CreateAllowListFunctions(ContractAddress)...)

	for name, function := range abiFunctionMap {
		method, ok := ACP224FeeManagerABI.Methods[name]
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
