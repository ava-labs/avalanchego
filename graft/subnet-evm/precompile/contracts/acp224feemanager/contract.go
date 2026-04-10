// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp224feemanager

import (
	"errors"
	"fmt"
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

//go:embed IACP224FeeManager.abi
var ACP224FeeManagerRawABI string

var ACP224FeeManagerABI = contract.ParseABI(ACP224FeeManagerRawABI)

const (
	numFeeConfigField = 5 // validatorTargetGas, targetGas, staticPricing, minGasPrice, timeToDouble

	getFeeConfigGasCost              uint64 = contract.ReadGasCostPerSlot
	getFeeConfigLastChangedAtGasCost uint64 = contract.ReadGasCostPerSlot
	// FeeConfigUpdated has 2 topics (event sig + indexed sender) and non-indexed
	// data containing old + new FeeConfig. Each config has numFeeConfigField
	// static fields, each ABI-encoded as a 32-byte word, so the total data size
	// is numFeeConfigField * 32 bytes * 2 configs.
	feeConfigUpdatedEventGasCost uint64 = contract.LogGas + contract.LogTopicGas*2 + contract.LogDataGas*numFeeConfigField*common.HashLength*2
	setFeeConfigGasCost          uint64 = allowlist.ReadAllowListGasCost +
		contract.WriteGasCostPerSlot*2 + // fee config slot + lastChangedAt slot
		getFeeConfigGasCost + // reading old config for event
		feeConfigUpdatedEventGasCost
)

var (
	errCannotSetFeeConfig = errors.New("non-enabled cannot call setFeeConfig")
	errInvalidABIConfig   = errors.New("failed to convert ABI config")
	errNilBlockNumber     = errors.New("block number cannot be nil")
)

// storageSlot returns a storage key with the "acp224" namespace prefix
// left-aligned in the hash. This avoids collisions with AllowList role
// storage, which right-aligns 20-byte addresses via BytesToHash and
// therefore always has 12 leading zero bytes.
func storageSlot(key ...byte) common.Hash {
	s := common.Hash{'a', 'c', 'p', '2', '2', '4'}
	copy(s[6:], key)
	return s
}

var (
	feeConfigStorageKey       = storageSlot('f', 'c')
	feeConfigLastChangedAtKey = storageSlot('l', 'c', 'a')
)

// GetACP224FeeManagerAllowListStatus returns the role of [address] for the allow list.
func GetACP224FeeManagerAllowListStatus(stateDB contract.StateReader, contractAddr common.Address, address common.Address) allowlist.Role {
	return allowlist.GetAllowListStatus(stateDB, contractAddr, address)
}

// SetACP224FeeManagerAllowListStatus assumes [role] has already been verified as valid.
// Roles are stored keyed by address hash, so precompile storage keys must not collide
// with address-derived keys.
func SetACP224FeeManagerAllowListStatus(stateDB contract.StateDB, contractAddr common.Address, address common.Address, role allowlist.Role) {
	allowlist.SetAllowListRole(stateDB, contractAddr, address, role)
}

// GetStoredFeeConfig returns the fee config from contract storage.
// Configure always stores a value during activation, so the caller
// MUST NOT call this before the precompile has been configured.
func GetStoredFeeConfig(stateDB contract.StateReader, contractAddr common.Address) commontype.ACP224FeeConfig {
	var cfg commontype.ACP224FeeConfig
	cfg.UnpackFrom(stateDB.GetState(contractAddr, feeConfigStorageKey))
	return cfg
}

// GetFeeConfigLastChangedAt returns the block number of the last fee config update.
func GetFeeConfigLastChangedAt(stateDB contract.StateReader, contractAddr common.Address) *big.Int {
	val := stateDB.GetState(contractAddr, feeConfigLastChangedAtKey)
	return val.Big()
}

// StoreFeeConfig validates and persists feeConfig and blockNumber to contract storage.
func StoreFeeConfig(stateDB contract.StateDB, contractAddr common.Address, feeConfig commontype.ACP224FeeConfig, blockNumber *big.Int) error {
	if err := feeConfig.Verify(); err != nil {
		return fmt.Errorf("cannot verify fee config: %w", err)
	}

	stateDB.SetState(contractAddr, feeConfigStorageKey, feeConfig.Pack())
	stateDB.SetState(contractAddr, feeConfigLastChangedAtKey, common.BigToHash(blockNumber))
	return nil
}

// PackGetFeeConfig packs the getFeeConfig calldata including the 4-byte selector.
func PackGetFeeConfig() ([]byte, error) {
	return ACP224FeeManagerABI.Pack("getFeeConfig")
}

// PackGetFeeConfigOutput ABI-encodes [config] as getFeeConfig return data.
func PackGetFeeConfigOutput(config commontype.ACP224FeeConfig) ([]byte, error) {
	return ACP224FeeManagerABI.PackOutput("getFeeConfig", config)
}

// UnpackGetFeeConfigOutput decodes ABI-encoded getFeeConfig return data.
func UnpackGetFeeConfigOutput(output []byte) (commontype.ACP224FeeConfig, error) {
	var config commontype.ACP224FeeConfig
	err := ACP224FeeManagerABI.UnpackIntoInterface(&config, "getFeeConfig", output)
	return config, err
}

//nolint:revive // unused params are part of RunStatefulPrecompileFunc signature
func getFeeConfig(
	accessibleState contract.AccessibleState,
	caller common.Address,
	self common.Address, // EVM-semantic self; see libevm.AddressContext
	input []byte,
	suppliedGas uint64,
	readOnly bool,
) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, getFeeConfigGasCost); err != nil {
		return nil, 0, err
	}

	feeConfig := GetStoredFeeConfig(accessibleState.GetStateDB(), self)

	output, err := PackGetFeeConfigOutput(feeConfig)
	if err != nil {
		return nil, remainingGas, err
	}

	return output, remainingGas, nil
}

// PackGetFeeConfigLastChangedAt packs the calldata including the 4-byte selector.
func PackGetFeeConfigLastChangedAt() ([]byte, error) {
	return ACP224FeeManagerABI.Pack("getFeeConfigLastChangedAt")
}

// PackGetFeeConfigLastChangedAtOutput ABI-encodes [blockNumber] as return data.
func PackGetFeeConfigLastChangedAtOutput(blockNumber *big.Int) ([]byte, error) {
	return ACP224FeeManagerABI.PackOutput("getFeeConfigLastChangedAt", blockNumber)
}

// UnpackGetFeeConfigLastChangedAtOutput decodes ABI-encoded return data.
func UnpackGetFeeConfigLastChangedAtOutput(output []byte) (*big.Int, error) {
	res, err := ACP224FeeManagerABI.Unpack("getFeeConfigLastChangedAt", output)
	if err != nil {
		return nil, err
	}
	unpacked := *abi.ConvertType(res[0], new(*big.Int)).(**big.Int)
	return unpacked, nil
}

//nolint:revive // unused params are part of RunStatefulPrecompileFunc signature
func getFeeConfigLastChangedAt(
	accessibleState contract.AccessibleState,
	caller common.Address,
	self common.Address, // EVM-semantic self; see libevm.AddressContext
	input []byte,
	suppliedGas uint64,
	readOnly bool,
) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, getFeeConfigLastChangedAtGasCost); err != nil {
		return nil, 0, err
	}

	lastChangedAt := GetFeeConfigLastChangedAt(accessibleState.GetStateDB(), self)
	packedOutput, err := PackGetFeeConfigLastChangedAtOutput(lastChangedAt)
	if err != nil {
		return nil, remainingGas, err
	}

	return packedOutput, remainingGas, nil
}

// PackSetFeeConfig packs [config] into ABI-encoded calldata including the 4-byte selector.
func PackSetFeeConfig(config commontype.ACP224FeeConfig) ([]byte, error) {
	return ACP224FeeManagerABI.Pack("setFeeConfig", config)
}

// UnpackSetFeeConfigInput assumes [input] does not include the 4-byte selector.
func UnpackSetFeeConfigInput(input []byte) (commontype.ACP224FeeConfig, error) {
	// UnpackInputIntoInterface doesn't work for single-tuple arguments: copyAtomic
	// tries to assign the whole tuple to the first struct field (a bool), causing a
	// type mismatch. Use method.Inputs.Unpack + abi.ConvertType instead.
	method, ok := ACP224FeeManagerABI.Methods["setFeeConfig"]
	if !ok {
		return commontype.ACP224FeeConfig{}, errors.New("method setFeeConfig not found")
	}
	res, err := method.Inputs.Unpack(input)
	if err != nil {
		return commontype.ACP224FeeConfig{}, err
	}
	config, ok := abi.ConvertType(res[0], new(commontype.ACP224FeeConfig)).(*commontype.ACP224FeeConfig)
	if !ok {
		return commontype.ACP224FeeConfig{}, errInvalidABIConfig
	}
	return *config, nil
}

func setFeeConfig(
	accessibleState contract.AccessibleState,
	caller common.Address,
	self common.Address, // EVM-semantic self; see libevm.AddressContext
	input []byte,
	suppliedGas uint64,
	readOnly bool,
) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, setFeeConfigGasCost); err != nil {
		return nil, 0, err
	}

	if readOnly {
		return nil, remainingGas, vm.ErrWriteProtection
	}

	stateDB := accessibleState.GetStateDB()
	callerStatus := GetACP224FeeManagerAllowListStatus(stateDB, self, caller)
	if !callerStatus.IsEnabled() {
		return nil, remainingGas, fmt.Errorf("%w: %s", errCannotSetFeeConfig, caller)
	}

	feeConfig, err := UnpackSetFeeConfigInput(input)
	if err != nil {
		return nil, remainingGas, err
	}

	blockNumber := accessibleState.GetBlockContext().Number()
	if blockNumber == nil {
		return nil, remainingGas, errNilBlockNumber
	}

	oldConfig := GetStoredFeeConfig(stateDB, self)

	if err := StoreFeeConfig(stateDB, self, feeConfig, blockNumber); err != nil {
		return nil, remainingGas, err
	}
	topics, data, err := PackFeeConfigUpdatedEvent(
		caller,
		oldConfig,
		feeConfig,
	)
	if err != nil {
		return nil, remainingGas, err
	}

	stateDB.AddLog(&types.Log{
		Address:     self,
		Topics:      topics,
		Data:        data,
		BlockNumber: blockNumber.Uint64(),
	})

	return []byte{}, remainingGas, nil
}

var acp224FeeManagerPrecompile = createACP224FeeManagerPrecompile()

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
	statefulContract, err := contract.NewStatefulPrecompileContract(nil, functions) // nil = no fallback
	if err != nil {
		panic(err)
	}
	return statefulContract
}
