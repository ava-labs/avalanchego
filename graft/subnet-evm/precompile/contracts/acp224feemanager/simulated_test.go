// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// simulated_test.go validates the ACP-224 fee manager precompile by routing
// calls through a deployed Solidity wrapper contract (ACP224FeeManagerTest.sol),
// which acts as contract as an integration test with the EVM interpreter:
//
//	Go abi.Pack() -> wrapper (solc-compiled) -> ABI re-encoding -> precompile -> Go abi.Unpack()
//
// catching any disagreement between Go's ABI package and solc's ABI
// implementation. Note that some tests overlap with contract_test.go (e.g. fee config
// validation) but the overlap is intentional: contract_test.go tests precompile
// logic directly, while these tests verify the ABI round-trip through the EVM
// interpreter.
//
// Also, errors from the precompile lose their identity when serialized as EVM revert
// data, so error assertions use ErrorContains instead of ErrorIs.

package acp224feemanager_test

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
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/acp224feemanager"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/acp224feemanager/acp224feemanagertest/bindings"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/utilstest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/modules"
	"github.com/ava-labs/avalanchego/utils"

	sim "github.com/ava-labs/avalanchego/graft/subnet-evm/ethclient/simulated"
)

var (
	adminKey, _  = crypto.GenerateKey()
	adminAddress = crypto.PubkeyToAddress(adminKey.PublicKey)
)

var (
	genesisFeeConfig = commontype.ACP224FeeConfig{
		TargetGas:    2_000_000,
		MinGasPrice:  25,
		TimeToDouble: 120,
	}

	updatedFeeConfig = commontype.ACP224FeeConfig{
		TargetGas:    5_000_000,
		MinGasPrice:  42,
		TimeToDouble: 300,
	}

	boolFeeConfig = commontype.ACP224FeeConfig{
		ValidatorTargetGas: true,
		StaticPricing:      true,
		MinGasPrice:        7,
	}

	testPrecompileConfig = acp224feemanager.NewConfig(
		utils.PointerTo[uint64](0),
		[]common.Address{adminAddress},
		nil, nil,
		&genesisFeeConfig,
	)
)

func TestMain(m *testing.M) {
	core.RegisterExtras()
	customtypes.Register()
	params.RegisterExtras()
	// TODO(JonathanOppenheimer): Remove manual registration when the
	// module is registered unconditionally in init().
	if err := modules.RegisterModule(acp224feemanager.Module); err != nil {
		panic(err)
	}
	m.Run()
}

func toBindingsFeeConfig(c commontype.ACP224FeeConfig) bindings.IACP224FeeManagerFeeConfig {
	return bindings.IACP224FeeManagerFeeConfig{
		ValidatorTargetGas: c.ValidatorTargetGas,
		TargetGas:          c.TargetGas,
		StaticPricing:      c.StaticPricing,
		MinGasPrice:        c.MinGasPrice,
		TimeToDouble:       c.TimeToDouble,
	}
}

func toCommonFeeConfig(c bindings.IACP224FeeManagerFeeConfig) commontype.ACP224FeeConfig {
	return commontype.ACP224FeeConfig{
		ValidatorTargetGas: c.ValidatorTargetGas,
		TargetGas:          c.TargetGas,
		StaticPricing:      c.StaticPricing,
		MinGasPrice:        c.MinGasPrice,
		TimeToDouble:       c.TimeToDouble,
	}
}

func deployTestContract(t *testing.T, b *sim.Backend, auth *bind.TransactOpts) (common.Address, *bindings.ACP224FeeManagerTest) {
	t.Helper()
	addr, tx, contract, err := bindings.DeployACP224FeeManagerTest(auth, b.Client(), acp224feemanager.ContractAddress)
	require.NoError(t, err, "DeployACP224FeeManagerTest()")
	utilstest.WaitReceiptSuccessful(t, b, tx)
	return addr, contract
}

func setFeeConfig(t *testing.T, b *sim.Backend, contract *bindings.ACP224FeeManagerTest, auth *bind.TransactOpts, config commontype.ACP224FeeConfig) uint64 {
	t.Helper()
	tx, err := contract.SetFeeConfig(auth, toBindingsFeeConfig(config))
	require.NoError(t, err, "SetFeeConfig()")
	receipt := utilstest.WaitReceiptSuccessful(t, b, tx)
	return receipt.BlockNumber.Uint64()
}

func TestACP224FeeManager(t *testing.T) {
	chainID := params.TestChainConfig.ChainID
	admin := utilstest.NewAuth(t, adminKey, chainID)

	type testCase struct {
		name string
		test func(t *testing.T, backend *sim.Backend, feeManager *bindings.IACP224FeeManager)
	}

	testCases := []testCase{
		{
			name: "should verify admin has admin role",
			test: func(t *testing.T, _ *sim.Backend, feeManager *bindings.IACP224FeeManager) {
				allowlisttest.VerifyRole(t, feeManager, adminAddress, allowlist.AdminRole)
			},
		},
		{
			name: "should verify new contract has no role",
			test: func(t *testing.T, backend *sim.Backend, feeManager *bindings.IACP224FeeManager) {
				testContractAddr, _ := deployTestContract(t, backend, admin)
				allowlisttest.VerifyRole(t, feeManager, testContractAddr, allowlist.NoRole)
			},
		},
		{
			name: "contract should not be able to change fee without enabled",
			test: func(t *testing.T, backend *sim.Backend, _ *bindings.IACP224FeeManager) {
				_, testContract := deployTestContract(t, backend, admin)

				_, err := testContract.SetFeeConfig(admin, toBindingsFeeConfig(updatedFeeConfig))
				require.ErrorContains(t, err, vm.ErrExecutionReverted.Error()) //nolint:forbidigo // upstream error wrapped as string
			},
		},
		{
			name: "contract should be added to enabled list",
			test: func(t *testing.T, backend *sim.Backend, feeManager *bindings.IACP224FeeManager) {
				testContractAddr, _ := deployTestContract(t, backend, admin)
				allowlisttest.SetAsEnabled(t, backend, feeManager, admin, testContractAddr)
				allowlisttest.VerifyRole(t, feeManager, testContractAddr, allowlist.EnabledRole)
			},
		},
		{
			name: "enabled contract should set and read fee config",
			test: func(t *testing.T, backend *sim.Backend, feeManager *bindings.IACP224FeeManager) {
				testContractAddr, testContract := deployTestContract(t, backend, admin)
				allowlisttest.SetAsEnabled(t, backend, feeManager, admin, testContractAddr)

				got, err := testContract.GetFeeConfig(nil)
				require.NoError(t, err, "GetFeeConfig() before set")
				require.Equal(t, genesisFeeConfig, toCommonFeeConfig(got), "genesis fee config")

				blockNum := setFeeConfig(t, backend, testContract, admin, updatedFeeConfig)

				got, err = testContract.GetFeeConfig(nil)
				require.NoError(t, err, "GetFeeConfig() after set")
				require.Equal(t, updatedFeeConfig, toCommonFeeConfig(got), "updated fee config")

				lastChangedAt, err := testContract.GetFeeConfigLastChangedAt(nil)
				require.NoError(t, err, "GetFeeConfigLastChangedAt()")
				require.Equal(t, blockNum, lastChangedAt.Uint64(), "last changed at block number")
			},
		},
		{
			name: "bool fields round-trip through contract",
			test: func(t *testing.T, backend *sim.Backend, feeManager *bindings.IACP224FeeManager) {
				testContractAddr, testContract := deployTestContract(t, backend, admin)
				allowlisttest.SetAsEnabled(t, backend, feeManager, admin, testContractAddr)

				setFeeConfig(t, backend, testContract, admin, boolFeeConfig)

				got, err := testContract.GetFeeConfig(nil)
				require.NoError(t, err, "GetFeeConfig()")
				require.Equal(t, boolFeeConfig, toCommonFeeConfig(got), "boolean fee config round-trip")
			},
		},
		{
			name: "contract role can be revoked",
			test: func(t *testing.T, backend *sim.Backend, feeManager *bindings.IACP224FeeManager) {
				testContractAddr, testContract := deployTestContract(t, backend, admin)
				allowlisttest.SetAsEnabled(t, backend, feeManager, admin, testContractAddr)

				setFeeConfig(t, backend, testContract, admin, updatedFeeConfig)

				got, err := testContract.GetFeeConfig(nil)
				require.NoError(t, err, "GetFeeConfig() after set")
				require.Equal(t, updatedFeeConfig, toCommonFeeConfig(got), "config set by enabled contract")

				allowlisttest.SetAsNone(t, backend, feeManager, admin, testContractAddr)
				allowlisttest.VerifyRole(t, feeManager, testContractAddr, allowlist.NoRole)

				_, err = testContract.SetFeeConfig(admin, toBindingsFeeConfig(boolFeeConfig))
				require.ErrorContains(t, err, vm.ErrExecutionReverted.Error()) //nolint:forbidigo // upstream error wrapped as string
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			backend := utilstest.NewBackendWithPrecompile(t, testPrecompileConfig, []common.Address{adminAddress})
			defer backend.Close()

			feeManager, err := bindings.NewIACP224FeeManager(acp224feemanager.ContractAddress, backend.Client())
			require.NoError(t, err, "NewIACP224FeeManager()")

			tc.test(t, backend, feeManager)
		})
	}
}

func TestACP224FeeManager_InvalidFeeConfig(t *testing.T) {
	// Validation errors from the precompile revert through the wrapper
	// contract, so only the generic "execution reverted" is visible. The
	// specific error messages are tested in contract_test.go.
	tests := []struct {
		name   string
		config bindings.IACP224FeeManagerFeeConfig
	}{
		{
			name: "zero MinGasPrice",
			config: bindings.IACP224FeeManagerFeeConfig{
				TargetGas:    1_000_000,
				TimeToDouble: 60,
			},
		},
		{
			name: "ValidatorTargetGas with non-zero TargetGas",
			config: bindings.IACP224FeeManagerFeeConfig{
				ValidatorTargetGas: true,
				TargetGas:          1_000_000,
				MinGasPrice:        1,
			},
		},
		{
			name: "StaticPricing with non-zero TimeToDouble",
			config: bindings.IACP224FeeManagerFeeConfig{
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

	feeManager, err := bindings.NewIACP224FeeManager(acp224feemanager.ContractAddress, backend.Client())
	require.NoError(t, err, "NewIACP224FeeManager()")

	testContractAddr, testContract := deployTestContract(t, backend, admin)
	allowlisttest.SetAsEnabled(t, backend, feeManager, admin, testContractAddr)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := testContract.SetFeeConfig(admin, tt.config)
			require.ErrorContains(t, err, vm.ErrExecutionReverted.Error()) //nolint:forbidigo // upstream error wrapped as string
		})
	}
}

func TestACP224FeeManager_Events(t *testing.T) {
	admin := utilstest.NewAuth(t, adminKey, params.TestChainConfig.ChainID)

	backend := utilstest.NewBackendWithPrecompile(t, testPrecompileConfig, []common.Address{adminAddress})
	defer backend.Close()

	feeManager, err := bindings.NewIACP224FeeManager(acp224feemanager.ContractAddress, backend.Client())
	require.NoError(t, err, "NewIACP224FeeManager()")

	testContractAddr, testContract := deployTestContract(t, backend, admin)
	allowlisttest.SetAsEnabled(t, backend, feeManager, admin, testContractAddr)

	setFeeConfig(t, backend, testContract, admin, updatedFeeConfig)

	// The event sender is the wrapper contract address, not adminAddress.
	iter, err := feeManager.FilterFeeConfigUpdated(nil, []common.Address{testContractAddr})
	require.NoError(t, err, "FilterFeeConfigUpdated()")
	defer iter.Close()

	require.True(t, iter.Next(), "expected FeeConfigUpdated event")
	event := iter.Event
	require.Equal(t, testContractAddr, event.Sender, "event sender")
	require.Equal(t, genesisFeeConfig, toCommonFeeConfig(event.OldFeeConfig), "old fee config in event")
	require.Equal(t, updatedFeeConfig, toCommonFeeConfig(event.NewFeeConfig), "new fee config in event")
	require.False(t, iter.Next(), "expected no more events")
	require.NoError(t, iter.Error(), "iterator error")
}
