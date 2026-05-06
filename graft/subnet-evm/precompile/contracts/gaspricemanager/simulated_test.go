// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// simulated_test.go validates the gas price manager precompile by routing
// calls through a deployed Solidity wrapper contract (GasPriceManagerTest.sol),
// which acts as contract as an integration test with the EVM interpreter:
//
//	Go abi.Pack() -> wrapper (solc-compiled) -> ABI re-encoding -> precompile -> Go abi.Unpack()
//
// catching any disagreement between Go's ABI package and solc's ABI
// implementation. Note that some tests overlap with contract_test.go (e.g. gas price config
// validation) but the overlap is intentional: contract_test.go tests precompile
// logic directly, while these tests verify the ABI round-trip through the EVM
// interpreter.
//
// Also, errors from the precompile lose their identity when serialized as EVM revert
// data, so error assertions use ErrorContains instead of ErrorIs.

package gaspricemanager_test

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi/bind"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist/allowlisttest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/gaspricemanager"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/gaspricemanager/gaspricemanagertest/bindings"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/utilstest"
	"github.com/ava-labs/avalanchego/utils"

	sim "github.com/ava-labs/avalanchego/graft/subnet-evm/ethclient/simulated"
)

var (
	adminKey, _  = crypto.GenerateKey()
	adminAddress = crypto.PubkeyToAddress(adminKey.PublicKey)
)

var (
	genesisGasPriceConfig = commontype.GasPriceConfig{
		TargetGas:    2_000_000,
		MinGasPrice:  25,
		TimeToDouble: 120,
	}

	updatedGasPriceConfig = commontype.GasPriceConfig{
		TargetGas:    5_000_000,
		MinGasPrice:  42,
		TimeToDouble: 300,
	}

	boolGasPriceConfig = commontype.GasPriceConfig{
		ValidatorTargetGas: true,
		StaticPricing:      true,
		MinGasPrice:        7,
	}

	testPrecompileConfig = gaspricemanager.NewConfig(
		utils.PointerTo[uint64](0),
		[]common.Address{adminAddress},
		nil, nil,
		&genesisGasPriceConfig,
	)
)

func TestMain(m *testing.M) {
	// Ensure libevm extras are registered for tests.
	core.RegisterExtras()
	customtypes.Register()
	params.RegisterExtras()
	m.Run()
}

func toBindingsGasPriceConfig(c commontype.GasPriceConfig) bindings.IGasPriceManagerGasPriceConfig {
	return bindings.IGasPriceManagerGasPriceConfig{
		ValidatorTargetGas: c.ValidatorTargetGas,
		TargetGas:          c.TargetGas,
		StaticPricing:      c.StaticPricing,
		MinGasPrice:        c.MinGasPrice,
		TimeToDouble:       c.TimeToDouble,
	}
}

func toCommonGasPriceConfig(c bindings.IGasPriceManagerGasPriceConfig) commontype.GasPriceConfig {
	return commontype.GasPriceConfig{
		ValidatorTargetGas: c.ValidatorTargetGas,
		TargetGas:          c.TargetGas,
		StaticPricing:      c.StaticPricing,
		MinGasPrice:        c.MinGasPrice,
		TimeToDouble:       c.TimeToDouble,
	}
}

func deployTestContract(t *testing.T, b *sim.Backend, auth *bind.TransactOpts) (common.Address, *bindings.GasPriceManagerTest) {
	t.Helper()
	addr, tx, contract, err := bindings.DeployGasPriceManagerTest(auth, b.Client(), gaspricemanager.ContractAddress)
	require.NoError(t, err, "DeployGasPriceManagerTest()")
	utilstest.WaitReceiptSuccessful(t, b, tx)
	return addr, contract
}

func setGasPriceConfig(t *testing.T, b *sim.Backend, contract *bindings.GasPriceManagerTest, auth *bind.TransactOpts, config commontype.GasPriceConfig) uint64 {
	t.Helper()
	tx, err := contract.SetGasPriceConfig(auth, toBindingsGasPriceConfig(config))
	require.NoError(t, err, "SetGasPriceConfig()")
	receipt := utilstest.WaitReceiptSuccessful(t, b, tx)
	return receipt.BlockNumber.Uint64()
}

func TestGasPriceManager(t *testing.T) {
	chainID := params.TestChainConfig.ChainID
	admin := utilstest.NewAuth(t, adminKey, chainID)

	type testCase struct {
		name string
		test func(t *testing.T, backend *sim.Backend, gasPriceManager *bindings.IGasPriceManager)
	}

	testCases := []testCase{
		{
			name: "should verify admin has admin role",
			test: func(t *testing.T, _ *sim.Backend, gasPriceManager *bindings.IGasPriceManager) {
				allowlisttest.VerifyRole(t, gasPriceManager, adminAddress, allowlist.AdminRole)
			},
		},
		{
			name: "should verify new contract has no role",
			test: func(t *testing.T, backend *sim.Backend, gasPriceManager *bindings.IGasPriceManager) {
				testContractAddr, _ := deployTestContract(t, backend, admin)
				allowlisttest.VerifyRole(t, gasPriceManager, testContractAddr, allowlist.NoRole)
			},
		},
		{
			name: "contract should not be able to change gas price config without enabled",
			test: func(t *testing.T, backend *sim.Backend, _ *bindings.IGasPriceManager) {
				_, testContract := deployTestContract(t, backend, admin)

				_, err := testContract.SetGasPriceConfig(admin, toBindingsGasPriceConfig(updatedGasPriceConfig))
				require.ErrorContains(t, err, vm.ErrExecutionReverted.Error(), "SetGasPriceConfig() without enabled role") //nolint:forbidigo // upstream error wrapped as string
			},
		},
		{
			name: "contract should be added to enabled list",
			test: func(t *testing.T, backend *sim.Backend, gasPriceManager *bindings.IGasPriceManager) {
				testContractAddr, _ := deployTestContract(t, backend, admin)
				allowlisttest.SetAsEnabled(t, backend, gasPriceManager, admin, testContractAddr)
				allowlisttest.VerifyRole(t, gasPriceManager, testContractAddr, allowlist.EnabledRole)
			},
		},
		{
			name: "enabled contract should set and read gas price config",
			test: func(t *testing.T, backend *sim.Backend, gasPriceManager *bindings.IGasPriceManager) {
				testContractAddr, testContract := deployTestContract(t, backend, admin)
				allowlisttest.SetAsEnabled(t, backend, gasPriceManager, admin, testContractAddr)

				got, err := testContract.GetGasPriceConfig(nil)
				require.NoError(t, err, "GetGasPriceConfig() before set")
				require.Equal(t, genesisGasPriceConfig, toCommonGasPriceConfig(got), "genesis gas price config")

				blockNum := setGasPriceConfig(t, backend, testContract, admin, updatedGasPriceConfig)

				got, err = testContract.GetGasPriceConfig(nil)
				require.NoError(t, err, "GetGasPriceConfig() after set")
				require.Equal(t, updatedGasPriceConfig, toCommonGasPriceConfig(got), "updated gas price config")

				lastChangedAt, err := testContract.GetGasPriceConfigLastChangedAt(nil)
				require.NoError(t, err, "GetGasPriceConfigLastChangedAt()")
				require.Equal(t, blockNum, lastChangedAt.Uint64(), "last changed at block number")
			},
		},
		{
			name: "bool fields round-trip through contract",
			test: func(t *testing.T, backend *sim.Backend, gasPriceManager *bindings.IGasPriceManager) {
				testContractAddr, testContract := deployTestContract(t, backend, admin)
				allowlisttest.SetAsEnabled(t, backend, gasPriceManager, admin, testContractAddr)

				setGasPriceConfig(t, backend, testContract, admin, boolGasPriceConfig)

				got, err := testContract.GetGasPriceConfig(nil)
				require.NoError(t, err, "GetGasPriceConfig()")
				require.Equal(t, boolGasPriceConfig, toCommonGasPriceConfig(got), "boolean gas price config round-trip")
			},
		},
		{
			name: "contract role can be revoked",
			test: func(t *testing.T, backend *sim.Backend, gasPriceManager *bindings.IGasPriceManager) {
				testContractAddr, testContract := deployTestContract(t, backend, admin)
				allowlisttest.SetAsEnabled(t, backend, gasPriceManager, admin, testContractAddr)

				setGasPriceConfig(t, backend, testContract, admin, updatedGasPriceConfig)

				got, err := testContract.GetGasPriceConfig(nil)
				require.NoError(t, err, "GetGasPriceConfig() after set")
				require.Equal(t, updatedGasPriceConfig, toCommonGasPriceConfig(got), "config set by enabled contract")

				allowlisttest.SetAsNone(t, backend, gasPriceManager, admin, testContractAddr)
				allowlisttest.VerifyRole(t, gasPriceManager, testContractAddr, allowlist.NoRole)

				_, err = testContract.SetGasPriceConfig(admin, toBindingsGasPriceConfig(boolGasPriceConfig))
				require.ErrorContains(t, err, vm.ErrExecutionReverted.Error(), "SetGasPriceConfig() after role revoked") //nolint:forbidigo // upstream error wrapped as string
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			backend := utilstest.NewBackendWithPrecompile(t, testPrecompileConfig, []common.Address{adminAddress})
			defer backend.Close()

			gasPriceManager, err := bindings.NewIGasPriceManager(gaspricemanager.ContractAddress, backend.Client())
			require.NoError(t, err, "NewIGasPriceManager()")

			tc.test(t, backend, gasPriceManager)
		})
	}
}

func TestGasPriceManager_InvalidGasPriceConfig(t *testing.T) {
	// Validation errors from the precompile revert through the wrapper
	// contract, so only the generic "execution reverted" is visible. The
	// specific error messages are tested in contract_test.go.
	tests := []struct {
		name   string
		config bindings.IGasPriceManagerGasPriceConfig
	}{
		{
			name: "zero MinGasPrice",
			config: bindings.IGasPriceManagerGasPriceConfig{
				TargetGas:    1_000_000,
				TimeToDouble: 60,
			},
		},
		{
			name: "ValidatorTargetGas with non-zero TargetGas",
			config: bindings.IGasPriceManagerGasPriceConfig{
				ValidatorTargetGas: true,
				TargetGas:          1_000_000,
				MinGasPrice:        1,
			},
		},
		{
			name: "StaticPricing with non-zero TimeToDouble",
			config: bindings.IGasPriceManagerGasPriceConfig{
				StaticPricing: true,
				TargetGas:     1_000_000,
				MinGasPrice:   1,
				TimeToDouble:  60,
			},
		},
	}

	admin := utilstest.NewAuth(t, adminKey, params.TestChainConfig.ChainID)

	backend := utilstest.NewBackendWithPrecompile(t, testPrecompileConfig, []common.Address{adminAddress})
	defer backend.Close()

	gasPriceManager, err := bindings.NewIGasPriceManager(gaspricemanager.ContractAddress, backend.Client())
	require.NoError(t, err, "NewIGasPriceManager()")

	testContractAddr, testContract := deployTestContract(t, backend, admin)
	allowlisttest.SetAsEnabled(t, backend, gasPriceManager, admin, testContractAddr)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := testContract.SetGasPriceConfig(admin, tt.config)
			require.ErrorContains(t, err, vm.ErrExecutionReverted.Error(), "SetGasPriceConfig(%+v)", tt.config) //nolint:forbidigo // upstream error wrapped as string
		})
	}
}

func TestGasPriceManager_Events(t *testing.T) {
	admin := utilstest.NewAuth(t, adminKey, params.TestChainConfig.ChainID)

	backend := utilstest.NewBackendWithPrecompile(t, testPrecompileConfig, []common.Address{adminAddress})
	defer backend.Close()

	gasPriceManager, err := bindings.NewIGasPriceManager(gaspricemanager.ContractAddress, backend.Client())
	require.NoError(t, err, "NewIGasPriceManager()")

	testContractAddr, testContract := deployTestContract(t, backend, admin)
	allowlisttest.SetAsEnabled(t, backend, gasPriceManager, admin, testContractAddr)

	setGasPriceConfig(t, backend, testContract, admin, updatedGasPriceConfig)

	// The event sender is the wrapper contract address, not adminAddress.
	iter, err := gasPriceManager.FilterGasPriceConfigUpdated(nil, []common.Address{testContractAddr})
	require.NoError(t, err, "FilterGasPriceConfigUpdated()")
	defer iter.Close()

	require.True(t, iter.Next(), "expected GasPriceConfigUpdated event")
	event := iter.Event
	require.Equal(t, testContractAddr, event.Sender, "event sender")
	require.Equal(t, genesisGasPriceConfig, toCommonGasPriceConfig(event.OldGasPriceConfig), "old gas price config in event")
	require.Equal(t, updatedGasPriceConfig, toCommonGasPriceConfig(event.NewGasPriceConfig), "new gas price config in event")
	require.False(t, iter.Next(), "expected no more events")
	require.NoError(t, iter.Error(), "iterator error")
}
