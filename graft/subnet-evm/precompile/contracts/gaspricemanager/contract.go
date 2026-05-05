// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gaspricemanager

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

//go:embed IGasPriceManager.abi
var GasPriceManagerRawABI string

var GasPriceManagerABI = contract.ParseABI(GasPriceManagerRawABI)

const (
	numGasPriceConfigField = 5 // validatorTargetGas, targetGas, staticPricing, minGasPrice, timeToDouble

	getGasPriceConfigGasCost              uint64 = contract.ReadGasCostPerSlot
	getGasPriceConfigLastChangedAtGasCost uint64 = contract.ReadGasCostPerSlot
	// GasPriceConfigUpdated has 2 topics (event sig + indexed sender) and non-indexed
	// data containing old + new GasPriceConfig. Each config has numGasPriceConfigField
	// static fields, each ABI-encoded as a 32-byte word, so the total data size
	// is numGasPriceConfigField * 32 bytes * 2 configs.
	gasPriceConfigUpdatedEventGasCost uint64 = contract.LogGas + contract.LogTopicGas*2 + contract.LogDataGas*numGasPriceConfigField*common.HashLength*2
	setGasPriceConfigGasCost          uint64 = allowlist.ReadAllowListGasCost +
		contract.WriteGasCostPerSlot*2 + // gas price config slot + lastChangedAt slot
		getGasPriceConfigGasCost + // reading old config for event
		gasPriceConfigUpdatedEventGasCost
)

var (
	errCannotSetGasPriceConfig = errors.New("non-enabled cannot call setGasPriceConfig")
	errInvalidABIConfig        = errors.New("failed to convert ABI config")
	errNilBlockNumber          = errors.New("block number cannot be nil")
)

var gasPriceManagerPrecompile = createGasPriceManagerPrecompile()

func createGasPriceManagerPrecompile() contract.StatefulPrecompiledContract {
	abiFunctionMap := map[string]contract.RunStatefulPrecompileFunc{
		"getGasPriceConfig":              getGasPriceConfig,
		"getGasPriceConfigLastChangedAt": getGasPriceConfigLastChangedAt,
		"setGasPriceConfig":              setGasPriceConfig,
	}
	functions := make([]*contract.StatefulPrecompileFunction, 0, len(abiFunctionMap)+len(allowlist.AllowListABI.Methods))
	functions = append(functions, allowlist.CreateAllowListFunctions(ContractAddress)...)

	for name, function := range abiFunctionMap {
		method, ok := GasPriceManagerABI.Methods[name]
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

// getGasPriceConfig

//nolint:revive // unused params are part of RunStatefulPrecompileFunc signature
func getGasPriceConfig(
	accessibleState contract.AccessibleState,
	caller common.Address,
	self common.Address, // EVM-semantic self; see libevm.AddressContext
	input []byte, // ignored
	suppliedGas uint64,
	readOnly bool, // ignored - method only reads
) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, getGasPriceConfigGasCost); err != nil {
		return nil, 0, err
	}

	gasPriceConfig := GetStoredGasPriceConfig(accessibleState.GetStateDB(), self)

	output, err := PackGetGasPriceConfigOutput(gasPriceConfig)
	if err != nil {
		return nil, remainingGas, err
	}

	return output, remainingGas, nil
}

// PackGetGasPriceConfig packs the getGasPriceConfig calldata including the 4-byte selector.
func PackGetGasPriceConfig() ([]byte, error) {
	return GasPriceManagerABI.Pack("getGasPriceConfig")
}

// PackGetGasPriceConfigOutput ABI-encodes [config] as getGasPriceConfig return data.
func PackGetGasPriceConfigOutput(config commontype.GasPriceConfig) ([]byte, error) {
	return GasPriceManagerABI.PackOutput("getGasPriceConfig", config)
}

// UnpackGetGasPriceConfigOutput decodes ABI-encoded getGasPriceConfig return data.
func UnpackGetGasPriceConfigOutput(output []byte) (commontype.GasPriceConfig, error) {
	var config commontype.GasPriceConfig
	err := GasPriceManagerABI.UnpackIntoInterface(&config, "getGasPriceConfig", output)
	return config, err
}

// getGasPriceConfigLastChangedAt

//nolint:revive // unused params are part of RunStatefulPrecompileFunc signature
func getGasPriceConfigLastChangedAt(
	accessibleState contract.AccessibleState,
	caller common.Address,
	self common.Address, // EVM-semantic self; see libevm.AddressContext
	input []byte, // ignored
	suppliedGas uint64,
	readOnly bool, // ignored - method only reads
) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, getGasPriceConfigLastChangedAtGasCost); err != nil {
		return nil, 0, err
	}

	lastChangedAt := GetGasPriceConfigLastChangedAt(accessibleState.GetStateDB(), self)
	packedOutput, err := PackGetGasPriceConfigLastChangedAtOutput(lastChangedAt)
	if err != nil {
		return nil, remainingGas, err
	}

	return packedOutput, remainingGas, nil
}

// PackGetGasPriceConfigLastChangedAt packs the calldata including the 4-byte selector.
func PackGetGasPriceConfigLastChangedAt() ([]byte, error) {
	return GasPriceManagerABI.Pack("getGasPriceConfigLastChangedAt")
}

// PackGetGasPriceConfigLastChangedAtOutput ABI-encodes [blockNumber] as return data.
func PackGetGasPriceConfigLastChangedAtOutput(blockNumber *big.Int) ([]byte, error) {
	return GasPriceManagerABI.PackOutput("getGasPriceConfigLastChangedAt", blockNumber)
}

// UnpackGetGasPriceConfigLastChangedAtOutput decodes ABI-encoded return data.
func UnpackGetGasPriceConfigLastChangedAtOutput(output []byte) (*big.Int, error) {
	res, err := GasPriceManagerABI.Unpack("getGasPriceConfigLastChangedAt", output)
	if err != nil {
		return nil, err
	}
	unpacked := *abi.ConvertType(res[0], new(*big.Int)).(**big.Int)
	return unpacked, nil
}

// setGasPriceConfig

func setGasPriceConfig(
	accessibleState contract.AccessibleState,
	caller common.Address,
	self common.Address, // EVM-semantic self; see libevm.AddressContext
	input []byte,
	suppliedGas uint64,
	readOnly bool,
) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, setGasPriceConfigGasCost); err != nil {
		return nil, 0, err
	}

	if readOnly {
		return nil, remainingGas, vm.ErrWriteProtection
	}

	stateDB := accessibleState.GetStateDB()
	callerStatus := GetGasPriceManagerAllowListStatus(stateDB, self, caller)
	if !callerStatus.IsEnabled() {
		return nil, remainingGas, fmt.Errorf("%w: %s", errCannotSetGasPriceConfig, caller)
	}

	gasPriceConfig, err := UnpackSetGasPriceConfigInput(input)
	if err != nil {
		return nil, remainingGas, err
	}

	blockNumber := accessibleState.GetBlockContext().Number()
	if blockNumber == nil {
		return nil, remainingGas, errNilBlockNumber
	}

	oldConfig := GetStoredGasPriceConfig(stateDB, self)

	if err := StoreGasPriceConfig(stateDB, self, gasPriceConfig, blockNumber); err != nil {
		return nil, remainingGas, err
	}
	topics, data, err := PackGasPriceConfigUpdatedEvent(
		caller,
		oldConfig,
		gasPriceConfig,
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

// PackSetGasPriceConfig packs [config] into ABI-encoded calldata including the 4-byte selector.
func PackSetGasPriceConfig(config commontype.GasPriceConfig) ([]byte, error) {
	return GasPriceManagerABI.Pack("setGasPriceConfig", config)
}

// UnpackSetGasPriceConfigInput assumes [input] does not include the 4-byte selector.
func UnpackSetGasPriceConfigInput(input []byte) (commontype.GasPriceConfig, error) {
	// UnpackInputIntoInterface doesn't work for single-tuple arguments: copyAtomic
	// tries to assign the whole tuple to the first struct field (a bool), causing a
	// type mismatch. Use method.Inputs.Unpack + abi.ConvertType instead.
	method, ok := GasPriceManagerABI.Methods["setGasPriceConfig"]
	if !ok {
		return commontype.GasPriceConfig{}, errors.New("method setGasPriceConfig not found")
	}
	res, err := method.Inputs.Unpack(input)
	if err != nil {
		return commontype.GasPriceConfig{}, err
	}
	config, ok := abi.ConvertType(res[0], new(commontype.GasPriceConfig)).(*commontype.GasPriceConfig)
	if !ok {
		return commontype.GasPriceConfig{}, errInvalidABIConfig
	}
	return *config, nil
}

// helpers

// storageSlot returns a storage key with the "gasprm" namespace prefix
// left-aligned in the hash. This avoids collisions with AllowList role
// storage, which right-aligns 20-byte addresses via BytesToHash and
// therefore always has 12 leading zero bytes.
func storageSlot(key ...byte) common.Hash {
	s := common.Hash{'g', 'a', 's', 'p', 'r', 'm'}
	copy(s[6:], key)
	return s
}

var (
	gasPriceConfigStorageKey       = storageSlot('g', 'p')
	gasPriceConfigLastChangedAtKey = storageSlot('l', 'c', 'a')
)

// GetGasPriceManagerAllowListStatus returns the role of `address` for the allowlist.
func GetGasPriceManagerAllowListStatus(stateDB contract.StateReader, contractAddr common.Address, address common.Address) allowlist.Role {
	return allowlist.GetAllowListStatus(stateDB, contractAddr, address)
}

// SetGasPriceManagerAllowListStatus assumes [role] has already been verified as valid.
// Roles are stored keyed by address hash, so precompile storage keys must not collide
// with address-derived keys.
func SetGasPriceManagerAllowListStatus(stateDB contract.StateDB, contractAddr common.Address, address common.Address, role allowlist.Role) {
	allowlist.SetAllowListRole(stateDB, contractAddr, address, role)
}

// GetStoredGasPriceConfig returns the gas price config from contract storage.
// Configure always stores a value during activation, so the caller
// MUST NOT call this before the precompile has been configured.
func GetStoredGasPriceConfig(stateDB contract.StateReader, contractAddr common.Address) commontype.GasPriceConfig {
	var cfg commontype.GasPriceConfig
	cfg.UnpackFrom(stateDB.GetState(contractAddr, gasPriceConfigStorageKey))
	return cfg
}

// GetGasPriceConfigLastChangedAt returns the block number of the last gas price config update.
func GetGasPriceConfigLastChangedAt(stateDB contract.StateReader, contractAddr common.Address) *big.Int {
	val := stateDB.GetState(contractAddr, gasPriceConfigLastChangedAtKey)
	return val.Big()
}

// StoreGasPriceConfig validates and persists gasPriceConfig and blockNumber to contract storage.
func StoreGasPriceConfig(stateDB contract.StateDB, contractAddr common.Address, gasPriceConfig commontype.GasPriceConfig, blockNumber *big.Int) error {
	if err := gasPriceConfig.Verify(); err != nil {
		return fmt.Errorf("cannot verify gas price config: %w", err)
	}

	stateDB.SetState(contractAddr, gasPriceConfigStorageKey, gasPriceConfig.Pack())
	stateDB.SetState(contractAddr, gasPriceConfigLastChangedAtKey, common.BigToHash(blockNumber))
	return nil
}
