// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp224feemanager

import (
	"errors"
	"fmt"
	"math"
	"math/big"

	"github.com/ava-labs/libevm/accounts/abi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"

	_ "embed"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contract"
)

const (
	// Gas costs for each function.
	numFeeConfigField = 5 // validatorTargetGas, targetGas, staticPricing, minGasPrice, timeToDouble
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
	ACP224FeeManagerPrecompile = createACP224FeeManagerPrecompile()

	// Storage layout uses namespaced keys to prevent collisions with allowlist and other precompile state.
	// AllowList uses common.BytesToHash(address.Bytes()) (zero-left-padded addresses) for role storage,
	// so we use distinct prefixes for fee config.
	validatorTargetGasStorageKey = common.Hash{'a', 'c', 'p', '2', '2', '4', 'v', 'g'}
	targetGasStorageKey          = common.Hash{'a', 'c', 'p', '2', '2', '4', 't', 'g'}
	staticPricingStorageKey      = common.Hash{'a', 'c', 'p', '2', '2', '4', 's', 'p'}
	minGasPriceStorageKey        = common.Hash{'a', 'c', 'p', '2', '2', '4', 'm', 'p'}
	timeToDoubleStorageKey       = common.Hash{'a', 'c', 'p', '2', '2', '4', 't', 'd'}
	feeConfigLastChangedAtKey    = common.Hash{'a', 'c', 'p', '2', '2', '4', 'l', 'c', 'a'}

	ErrCannotSetFeeConfig = errors.New("non-enabled cannot call setFeeConfig")
	ErrInvalidUint64      = errors.New("value is not a valid uint64")
	ErrNilBigInt          = errors.New("nil big.Int value")
	ErrNilBlockNumber     = errors.New("block number cannot be nil")

	// IACP224FeeManagerRawABI contains the raw ABI of ACP224FeeManager contract.
	//go:embed IACP224FeeManager.abi
	ACP224FeeManagerRawABI string

	ACP224FeeManagerABI = contract.ParseABI(ACP224FeeManagerRawABI)
)

// abiFeeConfig is the ABI-compatible representation of ACP224FeeConfig using *big.Int
// fields to match the Solidity uint256 types.
type abiFeeConfig struct {
	ValidatorTargetGas bool
	TargetGas          *big.Int
	StaticPricing      bool
	MinGasPrice        *big.Int
	TimeToDouble       *big.Int
}

func toABIFeeConfig(c commontype.ACP224FeeConfig) abiFeeConfig {
	return abiFeeConfig{
		ValidatorTargetGas: c.ValidatorTargetGas,
		TargetGas:          new(big.Int).SetUint64(c.TargetGas),
		StaticPricing:      c.StaticPricing,
		MinGasPrice:        new(big.Int).SetUint64(c.MinGasPrice),
		TimeToDouble:       new(big.Int).SetUint64(c.TimeToDouble),
	}
}

var maxUint64 = new(big.Int).SetUint64(math.MaxUint64)

func fromABIFeeConfig(c abiFeeConfig) (commontype.ACP224FeeConfig, error) {
	for _, field := range []struct {
		name string
		val  *big.Int
	}{
		{"targetGas", c.TargetGas},
		{"minGasPrice", c.MinGasPrice},
		{"timeToDouble", c.TimeToDouble},
	} {
		if field.val == nil {
			return commontype.ACP224FeeConfig{}, fmt.Errorf("%w: %s", ErrNilBigInt, field.name)
		}
		if field.val.Sign() < 0 || field.val.Cmp(maxUint64) > 0 {
			return commontype.ACP224FeeConfig{}, fmt.Errorf("%w: %s", ErrInvalidUint64, field.name)
		}
	}
	return commontype.ACP224FeeConfig{
		ValidatorTargetGas: c.ValidatorTargetGas,
		TargetGas:          c.TargetGas.Uint64(),
		StaticPricing:      c.StaticPricing,
		MinGasPrice:        c.MinGasPrice.Uint64(),
		TimeToDouble:       c.TimeToDouble.Uint64(),
	}, nil
}

// GetACP224FeeManagerAllowListStatus returns the role of [address] for the ACP224FeeManager list.
func GetACP224FeeManagerAllowListStatus(stateDB contract.StateReader, address common.Address) allowlist.Role {
	return allowlist.GetAllowListStatus(stateDB, ContractAddress, address)
}

// SetACP224FeeManagerAllowListStatus sets the permissions of [address] to [role] for the
// ACP224FeeManager list. Assumes [role] has already been verified as valid.
// This stores the [role] in the contract storage with address [ContractAddress]
// and [address] hash. It means that any reusage of the [address] key for different value
// conflicts with the same slot [role] is stored.
// Precompile implementations must use a different key than [address] for their storage.
func SetACP224FeeManagerAllowListStatus(stateDB contract.StateDB, address common.Address, role allowlist.Role) {
	allowlist.SetAllowListRole(stateDB, ContractAddress, address, role)
}

func hashToBool(h common.Hash) bool {
	return h != (common.Hash{})
}

func boolToHash(b bool) common.Hash {
	if b {
		return common.BigToHash(common.Big1)
	}
	return common.Hash{}
}

// GetStoredFeeConfig returns fee config from contract storage in given state
func GetStoredFeeConfig(stateDB contract.StateReader) commontype.ACP224FeeConfig {
	return commontype.ACP224FeeConfig{
		ValidatorTargetGas: hashToBool(stateDB.GetState(ContractAddress, validatorTargetGasStorageKey)),
		TargetGas:          stateDB.GetState(ContractAddress, targetGasStorageKey).Big().Uint64(),
		StaticPricing:      hashToBool(stateDB.GetState(ContractAddress, staticPricingStorageKey)),
		MinGasPrice:        stateDB.GetState(ContractAddress, minGasPriceStorageKey).Big().Uint64(),
		TimeToDouble:       stateDB.GetState(ContractAddress, timeToDoubleStorageKey).Big().Uint64(),
	}
}

// GetFeeConfigLastChangedAt returns the block number when the fee config was last changed
func GetFeeConfigLastChangedAt(stateDB contract.StateReader) *big.Int {
	val := stateDB.GetState(ContractAddress, feeConfigLastChangedAtKey)
	return val.Big()
}

// StoreFeeConfig stores given [feeConfig] and [blockNumber] to the [stateDB].
// A validation on [feeConfig] is done before storing.
func StoreFeeConfig(stateDB contract.StateDB, feeConfig commontype.ACP224FeeConfig, blockNumber *big.Int) error {
	if blockNumber == nil {
		return ErrNilBlockNumber
	}
	if err := feeConfig.Verify(); err != nil {
		return fmt.Errorf("cannot verify fee config: %w", err)
	}

	stateDB.SetState(ContractAddress, validatorTargetGasStorageKey, boolToHash(feeConfig.ValidatorTargetGas))
	stateDB.SetState(ContractAddress, targetGasStorageKey, common.BigToHash(new(big.Int).SetUint64(feeConfig.TargetGas)))
	stateDB.SetState(ContractAddress, staticPricingStorageKey, boolToHash(feeConfig.StaticPricing))
	stateDB.SetState(ContractAddress, minGasPriceStorageKey, common.BigToHash(new(big.Int).SetUint64(feeConfig.MinGasPrice)))
	stateDB.SetState(ContractAddress, timeToDoubleStorageKey, common.BigToHash(new(big.Int).SetUint64(feeConfig.TimeToDouble)))
	stateDB.SetState(ContractAddress, feeConfigLastChangedAtKey, common.BigToHash(blockNumber))
	return nil
}

// PackSetFeeConfig packs [config] of type commontype.ACP224FeeConfig into the appropriate arguments for setFeeConfig.
// the packed bytes include selector (first 4 func signature bytes).
// This function is mostly used for tests.
func PackSetFeeConfig(config commontype.ACP224FeeConfig) ([]byte, error) {
	return ACP224FeeManagerABI.Pack("setFeeConfig", toABIFeeConfig(config))
}

// UnpackSetFeeConfigInput attempts to unpack [input] into the commontype.ACP224FeeConfig type argument
// assumes that [input] does not include selector (omits first 4 func signature bytes)
func UnpackSetFeeConfigInput(input []byte) (commontype.ACP224FeeConfig, error) {
	// Note: UnpackInputIntoInterface doesn't work here because setFeeConfig has a single
	// tuple argument. Internally it routes through copyAtomic which tries to assign the
	// entire unpacked tuple to the first struct field (a bool), causing a type mismatch.
	// Instead, we unpack via the method's Inputs and use abi.ConvertType.
	method, ok := ACP224FeeManagerABI.Methods["setFeeConfig"]
	if !ok {
		return commontype.ACP224FeeConfig{}, errors.New("method setFeeConfig not found")
	}
	res, err := method.Inputs.Unpack(input)
	if err != nil {
		return commontype.ACP224FeeConfig{}, err
	}
	abiConfig := *abi.ConvertType(res[0], new(abiFeeConfig)).(*abiFeeConfig)
	return fromABIFeeConfig(abiConfig)
}

func setFeeConfig(
	accessibleState contract.AccessibleState,
	caller common.Address,
	_ common.Address,
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
	callerStatus := GetACP224FeeManagerAllowListStatus(stateDB, caller)
	if !callerStatus.IsEnabled() {
		return nil, remainingGas, fmt.Errorf("%w: %s", ErrCannotSetFeeConfig, caller)
	}

	oldConfig := GetStoredFeeConfig(stateDB)
	topics, data, err := PackFeeConfigUpdatedEvent(
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

	if err := StoreFeeConfig(stateDB, feeConfig, accessibleState.GetBlockContext().Number()); err != nil {
		return nil, remainingGas, err
	}

	// Return an empty output and the remaining gas
	return []byte{}, remainingGas, nil
}

// PackGetFeeConfig packs the input including selector (first 4 func signature bytes).
// This function is mostly used for tests.
func PackGetFeeConfig() ([]byte, error) {
	return ACP224FeeManagerABI.Pack("getFeeConfig")
}

// PackGetFeeConfigOutput attempts to pack given config of type commontype.ACP224FeeConfig
// to conform the ABI outputs.
func PackGetFeeConfigOutput(config commontype.ACP224FeeConfig) ([]byte, error) {
	return ACP224FeeManagerABI.PackOutput("getFeeConfig", toABIFeeConfig(config))
}

// UnpackGetFeeConfigOutput attempts to unpack given [output] into the commontype.ACP224FeeConfig type output
// assumes that [output] does not include selector (omits first 4 func signature bytes)
func UnpackGetFeeConfigOutput(output []byte) (commontype.ACP224FeeConfig, error) {
	// Use Unpack + ConvertType instead of UnpackIntoInterface to avoid the
	// copyAtomic bug with single tuple returns (same issue as UnpackSetFeeConfigInput).
	res, err := ACP224FeeManagerABI.Unpack("getFeeConfig", output)
	if err != nil {
		return commontype.ACP224FeeConfig{}, err
	}
	abiConfig := *abi.ConvertType(res[0], new(abiFeeConfig)).(*abiFeeConfig)
	return fromABIFeeConfig(abiConfig)
}

func getFeeConfig(
	accessibleState contract.AccessibleState,
	_ common.Address,
	_ common.Address,
	_ []byte,
	suppliedGas uint64,
	_ bool,
) (ret []byte, remainingGas uint64, err error) {
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

// PackGetFeeConfigLastChangedAt packs the input including selector (first 4 func signature bytes).
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
	_ common.Address,
	_ []byte,
	suppliedGas uint64,
	_ bool,
) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, GetFeeConfigLastChangedAtGasCost); err != nil {
		return nil, 0, err
	}

	lastChangedAt := GetFeeConfigLastChangedAt(accessibleState.GetStateDB())
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
