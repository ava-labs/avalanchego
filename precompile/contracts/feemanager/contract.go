// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package feemanager

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"

	_ "embed"

	"github.com/ava-labs/subnet-evm/accounts/abi"
	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/subnet-evm/precompile/contract"
)

const (
	minFeeConfigFieldKey = iota + 1
	// add new fields below this
	// must preserve order of these fields
	gasLimitKey = iota
	targetBlockRateKey
	minBaseFeeKey
	targetGasKey
	baseFeeChangeDenominatorKey
	minBlockGasCostKey
	maxBlockGasCostKey
	blockGasCostStepKey
	// add new fields above this
	numFeeConfigField = iota - 1

	// [numFeeConfigField] fields in FeeConfig struct
	feeConfigInputLen = common.HashLength * numFeeConfigField

	SetFeeConfigGasCost     uint64 = contract.WriteGasCostPerSlot * (numFeeConfigField + 1) // plus one for setting last changed at
	GetFeeConfigGasCost     uint64 = contract.ReadGasCostPerSlot * numFeeConfigField
	GetLastChangedAtGasCost uint64 = contract.ReadGasCostPerSlot
)

var (

	// Singleton StatefulPrecompiledContract for setting fee configs by permissioned callers.
	FeeManagerPrecompile contract.StatefulPrecompiledContract = createFeeManagerPrecompile()

	feeConfigLastChangedAtKey = common.Hash{'l', 'c', 'a'}

	ErrCannotChangeFee = errors.New("non-enabled cannot change fee config")
	ErrInvalidLen      = errors.New("invalid input length for fee config Input")

	// IFeeManagerRawABI contains the raw ABI of FeeManager contract.
	//go:embed contract.abi
	FeeManagerRawABI string

	FeeManagerABI = contract.ParseABI(FeeManagerRawABI)
)

// FeeConfigABIStruct is the ABI struct for FeeConfig type.
type FeeConfigABIStruct struct {
	GasLimit                 *big.Int
	TargetBlockRate          *big.Int
	MinBaseFee               *big.Int
	TargetGas                *big.Int
	BaseFeeChangeDenominator *big.Int
	MinBlockGasCost          *big.Int
	MaxBlockGasCost          *big.Int
	BlockGasCostStep         *big.Int
}

// GetFeeManagerStatus returns the role of [address] for the fee config manager list.
func GetFeeManagerStatus(stateDB contract.StateReader, address common.Address) allowlist.Role {
	return allowlist.GetAllowListStatus(stateDB, ContractAddress, address)
}

// SetFeeManagerStatus sets the permissions of [address] to [role] for the
// fee config manager list. assumes [role] has already been verified as valid.
func SetFeeManagerStatus(stateDB contract.StateDB, address common.Address, role allowlist.Role) {
	allowlist.SetAllowListRole(stateDB, ContractAddress, address, role)
}

// GetStoredFeeConfig returns fee config from contract storage in given state
func GetStoredFeeConfig(stateDB contract.StateReader) commontype.FeeConfig {
	feeConfig := commontype.FeeConfig{}
	for i := minFeeConfigFieldKey; i <= numFeeConfigField; i++ {
		val := stateDB.GetState(ContractAddress, common.Hash{byte(i)})
		switch i {
		case gasLimitKey:
			feeConfig.GasLimit = new(big.Int).Set(val.Big())
		case targetBlockRateKey:
			feeConfig.TargetBlockRate = val.Big().Uint64()
		case minBaseFeeKey:
			feeConfig.MinBaseFee = new(big.Int).Set(val.Big())
		case targetGasKey:
			feeConfig.TargetGas = new(big.Int).Set(val.Big())
		case baseFeeChangeDenominatorKey:
			feeConfig.BaseFeeChangeDenominator = new(big.Int).Set(val.Big())
		case minBlockGasCostKey:
			feeConfig.MinBlockGasCost = new(big.Int).Set(val.Big())
		case maxBlockGasCostKey:
			feeConfig.MaxBlockGasCost = new(big.Int).Set(val.Big())
		case blockGasCostStepKey:
			feeConfig.BlockGasCostStep = new(big.Int).Set(val.Big())
		default:
			// This should never encounter an unknown fee config key
			panic(fmt.Sprintf("unknown fee config key: %d", i))
		}
	}
	return feeConfig
}

func GetFeeConfigLastChangedAt(stateDB contract.StateReader) *big.Int {
	val := stateDB.GetState(ContractAddress, feeConfigLastChangedAtKey)
	return val.Big()
}

// StoreFeeConfig stores given [feeConfig] and block number in the [blockContext] to the [stateDB].
// A validation on [feeConfig] is done before storing.
func StoreFeeConfig(stateDB contract.StateDB, feeConfig commontype.FeeConfig, blockContext contract.ConfigurationBlockContext) error {
	if err := feeConfig.Verify(); err != nil {
		return fmt.Errorf("cannot verify fee config: %w", err)
	}

	for i := minFeeConfigFieldKey; i <= numFeeConfigField; i++ {
		var input common.Hash
		switch i {
		case gasLimitKey:
			input = common.BigToHash(feeConfig.GasLimit)
		case targetBlockRateKey:
			input = common.BigToHash(new(big.Int).SetUint64(feeConfig.TargetBlockRate))
		case minBaseFeeKey:
			input = common.BigToHash(feeConfig.MinBaseFee)
		case targetGasKey:
			input = common.BigToHash(feeConfig.TargetGas)
		case baseFeeChangeDenominatorKey:
			input = common.BigToHash(feeConfig.BaseFeeChangeDenominator)
		case minBlockGasCostKey:
			input = common.BigToHash(feeConfig.MinBlockGasCost)
		case maxBlockGasCostKey:
			input = common.BigToHash(feeConfig.MaxBlockGasCost)
		case blockGasCostStepKey:
			input = common.BigToHash(feeConfig.BlockGasCostStep)
		default:
			// This should never encounter an unknown fee config key
			panic(fmt.Sprintf("unknown fee config key: %d", i))
		}
		stateDB.SetState(ContractAddress, common.Hash{byte(i)}, input)
	}

	blockNumber := blockContext.Number()
	if blockNumber == nil {
		return errors.New("blockNumber cannot be nil")
	}
	stateDB.SetState(ContractAddress, feeConfigLastChangedAtKey, common.BigToHash(blockNumber))
	return nil
}

// PackSetFeeConfig packs [inputStruct] of type SetFeeConfigInput into the appropriate arguments for setFeeConfig.
func PackSetFeeConfig(input commontype.FeeConfig) ([]byte, error) {
	inputStruct := FeeConfigABIStruct{
		GasLimit:                 input.GasLimit,
		TargetBlockRate:          new(big.Int).SetUint64(input.TargetBlockRate),
		MinBaseFee:               input.MinBaseFee,
		TargetGas:                input.TargetGas,
		BaseFeeChangeDenominator: input.BaseFeeChangeDenominator,
		MinBlockGasCost:          input.MinBlockGasCost,
		MaxBlockGasCost:          input.MaxBlockGasCost,
		BlockGasCostStep:         input.BlockGasCostStep,
	}
	return FeeManagerABI.Pack("setFeeConfig", inputStruct.GasLimit, inputStruct.TargetBlockRate, inputStruct.MinBaseFee, inputStruct.TargetGas, inputStruct.BaseFeeChangeDenominator, inputStruct.MinBlockGasCost, inputStruct.MaxBlockGasCost, inputStruct.BlockGasCostStep)
}

// UnpackSetFeeConfigInput attempts to unpack [input] as SetFeeConfigInput
// assumes that [input] does not include selector (omits first 4 func signature bytes)
// if [useStrictMode] is true, it will return an error if the length of [input] is not [feeConfigInputLen]
func UnpackSetFeeConfigInput(input []byte, useStrictMode bool) (commontype.FeeConfig, error) {
	// Initially we had this check to ensure that the input was the correct length.
	// However solidity does not always pack the input to the correct length, and allows
	// for extra padding bytes to be added to the end of the input. Therefore, we have removed
	// this check with the Durango. We still need to keep this check for backwards compatibility.
	if useStrictMode && len(input) != feeConfigInputLen {
		return commontype.FeeConfig{}, fmt.Errorf("%w: %d", ErrInvalidLen, len(input))
	}
	inputStruct := FeeConfigABIStruct{}
	err := FeeManagerABI.UnpackInputIntoInterface(&inputStruct, "setFeeConfig", input, useStrictMode)
	if err != nil {
		return commontype.FeeConfig{}, err
	}

	result := commontype.FeeConfig{
		GasLimit:                 inputStruct.GasLimit,
		TargetBlockRate:          inputStruct.TargetBlockRate.Uint64(),
		MinBaseFee:               inputStruct.MinBaseFee,
		TargetGas:                inputStruct.TargetGas,
		BaseFeeChangeDenominator: inputStruct.BaseFeeChangeDenominator,
		MinBlockGasCost:          inputStruct.MinBlockGasCost,
		MaxBlockGasCost:          inputStruct.MaxBlockGasCost,
		BlockGasCostStep:         inputStruct.BlockGasCostStep,
	}

	return result, nil
}

// setFeeConfig checks if the caller has permissions to set the fee config.
// The execution function parses [input] into FeeConfig structure and sets contract storage accordingly.
func setFeeConfig(accessibleState contract.AccessibleState, caller common.Address, _ common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, SetFeeConfigGasCost); err != nil {
		return nil, 0, err
	}

	if readOnly {
		return nil, remainingGas, vm.ErrWriteProtection
	}

	// do not use strict mode after Durango
	useStrictMode := !contract.IsDurangoActivated(accessibleState)
	feeConfig, err := UnpackSetFeeConfigInput(input, useStrictMode)
	if err != nil {
		return nil, remainingGas, err
	}

	stateDB := accessibleState.GetStateDB()
	// Verify that the caller is in the allow list and therefore has the right to call this function.
	callerStatus := GetFeeManagerStatus(stateDB, caller)
	if !callerStatus.IsEnabled() {
		return nil, remainingGas, fmt.Errorf("%w: %s", ErrCannotChangeFee, caller)
	}

	if contract.IsDurangoActivated(accessibleState) {
		if remainingGas, err = contract.DeductGas(remainingGas, FeeConfigChangedEventGasCost); err != nil {
			return nil, 0, err
		}
		oldConfig := GetStoredFeeConfig(stateDB)
		topics, data, err := PackFeeConfigChangedEvent(
			caller,
			oldConfig,
			feeConfig,
		)
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

	if err := StoreFeeConfig(stateDB, feeConfig, accessibleState.GetBlockContext()); err != nil {
		return nil, remainingGas, err
	}

	// Return an empty output and the remaining gas
	return []byte{}, remainingGas, nil
}

// PackGetFeeConfig packs the include selector (first 4 func signature bytes).
// This function is mostly used for tests.
func PackGetFeeConfig() ([]byte, error) {
	return FeeManagerABI.Pack("getFeeConfig")
}

// PackGetFeeConfigOutput attempts to pack given [outputStruct] of type GetFeeConfigOutput
// to conform the ABI outputs.
func PackGetFeeConfigOutput(output commontype.FeeConfig) ([]byte, error) {
	outputStruct := FeeConfigABIStruct{
		GasLimit:                 output.GasLimit,
		TargetBlockRate:          new(big.Int).SetUint64(output.TargetBlockRate),
		MinBaseFee:               output.MinBaseFee,
		TargetGas:                output.TargetGas,
		BaseFeeChangeDenominator: output.BaseFeeChangeDenominator,
		MinBlockGasCost:          output.MinBlockGasCost,
		MaxBlockGasCost:          output.MaxBlockGasCost,
		BlockGasCostStep:         output.BlockGasCostStep,
	}
	return FeeManagerABI.PackOutput("getFeeConfig",
		outputStruct.GasLimit,
		outputStruct.TargetBlockRate,
		outputStruct.MinBaseFee,
		outputStruct.TargetGas,
		outputStruct.BaseFeeChangeDenominator,
		outputStruct.MinBlockGasCost,
		outputStruct.MaxBlockGasCost,
		outputStruct.BlockGasCostStep,
	)
}

// UnpackGetFeeConfigOutput attempts to unpack [output] as GetFeeConfigOutput
// assumes that [output] does not include selector (omits first 4 func signature bytes)
func UnpackGetFeeConfigOutput(output []byte, skipLenCheck bool) (commontype.FeeConfig, error) {
	if !skipLenCheck && len(output) != feeConfigInputLen {
		return commontype.FeeConfig{}, fmt.Errorf("%w: %d", ErrInvalidLen, len(output))
	}
	outputStruct := FeeConfigABIStruct{}
	err := FeeManagerABI.UnpackIntoInterface(&outputStruct, "getFeeConfig", output)
	if err != nil {
		return commontype.FeeConfig{}, err
	}

	result := commontype.FeeConfig{
		GasLimit:                 outputStruct.GasLimit,
		TargetBlockRate:          outputStruct.TargetBlockRate.Uint64(),
		MinBaseFee:               outputStruct.MinBaseFee,
		TargetGas:                outputStruct.TargetGas,
		BaseFeeChangeDenominator: outputStruct.BaseFeeChangeDenominator,
		MinBlockGasCost:          outputStruct.MinBlockGasCost,
		MaxBlockGasCost:          outputStruct.MaxBlockGasCost,
		BlockGasCostStep:         outputStruct.BlockGasCostStep,
	}
	return result, nil
}

// getFeeConfig returns the stored fee config as an output.
// The execution function reads the contract state for the stored fee config and returns the output.
//
//nolint:revive // General-purpose types lose the meaning of args if unused ones are removed
func getFeeConfig(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, GetFeeConfigGasCost); err != nil {
		return nil, 0, err
	}

	feeConfig := GetStoredFeeConfig(accessibleState.GetStateDB())

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
	return FeeManagerABI.Pack("getFeeConfigLastChangedAt")
}

// PackGetFeeConfigLastChangedAtOutput attempts to pack given blockNumber of type *big.Int
// to conform the ABI outputs.
func PackGetFeeConfigLastChangedAtOutput(blockNumber *big.Int) ([]byte, error) {
	return FeeManagerABI.PackOutput("getFeeConfigLastChangedAt", blockNumber)
}

// UnpackGetFeeConfigLastChangedAtOutput attempts to unpack given [output] into the *big.Int type output
// assumes that [output] does not include selector (omits first 4 func signature bytes)
func UnpackGetFeeConfigLastChangedAtOutput(output []byte) (*big.Int, error) {
	res, err := FeeManagerABI.Unpack("getFeeConfigLastChangedAt", output)
	if err != nil {
		return new(big.Int), err
	}
	unpacked := *abi.ConvertType(res[0], new(*big.Int)).(**big.Int)
	return unpacked, nil
}

// getFeeConfigLastChangedAt returns the block number that fee config was last changed in.
// The execution function reads the contract state for the stored block number and returns the output.
//
//nolint:revive // General-purpose types lose the meaning of args if unused ones are removed
func getFeeConfigLastChangedAt(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, GetLastChangedAtGasCost); err != nil {
		return nil, 0, err
	}

	lastChangedAt := GetFeeConfigLastChangedAt(accessibleState.GetStateDB())
	packedOutput, err := PackGetFeeConfigLastChangedAtOutput(lastChangedAt)
	if err != nil {
		return nil, remainingGas, err
	}

	return packedOutput, remainingGas, err
}

// createFeeManagerPrecompile returns a StatefulPrecompiledContract with getters and setters for the precompile.
// Access to the getters/setters is controlled by an allow list for ContractAddress.
func createFeeManagerPrecompile() contract.StatefulPrecompiledContract {
	var functions []*contract.StatefulPrecompileFunction
	functions = append(functions, allowlist.CreateAllowListFunctions(ContractAddress)...)

	abiFunctionMap := map[string]contract.RunStatefulPrecompileFunc{
		"getFeeConfig":              getFeeConfig,
		"getFeeConfigLastChangedAt": getFeeConfigLastChangedAt,
		"setFeeConfig":              setFeeConfig,
	}

	for name, function := range abiFunctionMap {
		method, ok := FeeManagerABI.Methods[name]
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
