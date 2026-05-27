// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// simulated_test.go validates the gas price manager precompile by routing
// calls through a deployed Solidity wrapper contract (GasPriceManagerTest.sol),
// which acts as an integration test with the EVM interpreter:
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
	"os"
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
	os.Exit(m.Run())
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

// SUT is the system under test, primarily the [bindings.IGasPriceManager]
// binding to the gas price manager precompile.
type SUT struct {
	backend         *sim.Backend
	gasPriceManager *bindings.IGasPriceManager
	admin           *bind.TransactOpts
}

func newSUT(t *testing.T) *SUT {
	t.Helper()
	backend := utilstest.NewBackendWithPrecompile(t, testPrecompileConfig, []common.Address{adminAddress})
	t.Cleanup(func() {
		require.NoError(t, backend.Close(), "backend.Close()")
	})

	gasPriceManager, err := bindings.NewIGasPriceManager(gaspricemanager.ContractAddress, backend.Client())
	require.NoError(t, err, "NewIGasPriceManager()")

	return &SUT{
		backend:         backend,
		gasPriceManager: gasPriceManager,
		admin:           utilstest.NewAuth(t, adminKey, params.TestChainConfig.ChainID),
	}
}

func (s *SUT) deployTestContract(t *testing.T) (common.Address, *bindings.GasPriceManagerTest) {
	t.Helper()
	addr, tx, contract, err := bindings.DeployGasPriceManagerTest(s.admin, s.backend.Client(), gaspricemanager.ContractAddress)
	require.NoError(t, err, "DeployGasPriceManagerTest()")
	utilstest.WaitReceiptSuccessful(t, s.backend, tx)
	return addr, contract
}

// setGasPriceConfig routes the call through the deployed wrapper contract
// rather than the IGasPriceManager binding directly exercising the ABI
// round-trip these tests exist to verify.
func (s *SUT) setGasPriceConfig(t *testing.T, contract *bindings.GasPriceManagerTest, config commontype.GasPriceConfig) uint64 {
	t.Helper()
	tx, err := contract.SetGasPriceConfig(s.admin, toBindingsGasPriceConfig(config))
	require.NoError(t, err, "SetGasPriceConfig()")
	receipt := utilstest.WaitReceiptSuccessful(t, s.backend, tx)
	return receipt.BlockNumber.Uint64()
}

func TestGasPriceManager_InitialRoles(t *testing.T) {
	s := newSUT(t)
	testContractAddr, _ := s.deployTestContract(t)

	tests := []struct {
		name string
		addr common.Address
		want allowlist.Role
	}{
		{
			name: "admin has admin role",
			addr: adminAddress,
			want: allowlist.AdminRole,
		},
		{
			name: "freshly deployed contract has no role",
			addr: testContractAddr,
			want: allowlist.NoRole,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowlisttest.VerifyRole(t, s.gasPriceManager, tt.addr, tt.want)
		})
	}
}

func TestGasPriceManager_UnenabledContractCannotSetConfig(t *testing.T) {
	s := newSUT(t)
	_, testContract := s.deployTestContract(t)

	_, err := testContract.SetGasPriceConfig(s.admin, toBindingsGasPriceConfig(updatedGasPriceConfig))
	require.ErrorContains(t, err, vm.ErrExecutionReverted.Error(), "SetGasPriceConfig() without enabled role") //nolint:forbidigo // upstream error wrapped as string
}

func TestGasPriceManager_EnabledContractSetsAndModifiesConfig(t *testing.T) {
	s := newSUT(t)
	testContractAddr, testContract := s.deployTestContract(t)
	allowlisttest.SetAsEnabled(t, s.backend, s.gasPriceManager, s.admin, testContractAddr)

	got, err := testContract.GetGasPriceConfig(nil)
	require.NoError(t, err, "GetGasPriceConfig() before set")
	require.Equal(t, genesisGasPriceConfig, toCommonGasPriceConfig(got), "genesis gas price config")

	blockNum := s.setGasPriceConfig(t, testContract, updatedGasPriceConfig)

	got, err = testContract.GetGasPriceConfig(nil)
	require.NoError(t, err, "GetGasPriceConfig() after set")
	require.Equal(t, updatedGasPriceConfig, toCommonGasPriceConfig(got), "updated gas price config")

	lastChangedAt, err := testContract.GetGasPriceConfigLastChangedAt(nil)
	require.NoError(t, err, "GetGasPriceConfigLastChangedAt()")
	require.Equal(t, blockNum, lastChangedAt.Uint64(), "last changed at block number")
}

func TestGasPriceManager_BoolFieldsRoundTrip(t *testing.T) {
	s := newSUT(t)
	testContractAddr, testContract := s.deployTestContract(t)
	allowlisttest.SetAsEnabled(t, s.backend, s.gasPriceManager, s.admin, testContractAddr)

	s.setGasPriceConfig(t, testContract, boolGasPriceConfig)

	got, err := testContract.GetGasPriceConfig(nil)
	require.NoError(t, err, "GetGasPriceConfig()")
	require.Equal(t, boolGasPriceConfig, toCommonGasPriceConfig(got), "boolean gas price config round-trip")
}

func TestGasPriceManager_RoleCanBeRevoked(t *testing.T) {
	s := newSUT(t)
	testContractAddr, testContract := s.deployTestContract(t)
	allowlisttest.SetAsEnabled(t, s.backend, s.gasPriceManager, s.admin, testContractAddr)

	s.setGasPriceConfig(t, testContract, updatedGasPriceConfig)

	got, err := testContract.GetGasPriceConfig(nil)
	require.NoError(t, err, "GetGasPriceConfig() after set")
	require.Equal(t, updatedGasPriceConfig, toCommonGasPriceConfig(got), "config set by enabled contract")

	allowlisttest.SetAsNone(t, s.backend, s.gasPriceManager, s.admin, testContractAddr)
	allowlisttest.VerifyRole(t, s.gasPriceManager, testContractAddr, allowlist.NoRole)

	_, err = testContract.SetGasPriceConfig(s.admin, toBindingsGasPriceConfig(boolGasPriceConfig))
	require.ErrorContains(t, err, vm.ErrExecutionReverted.Error(), "SetGasPriceConfig() after role revoked") //nolint:forbidigo // upstream error wrapped as string
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

	s := newSUT(t)
	testContractAddr, testContract := s.deployTestContract(t)
	allowlisttest.SetAsEnabled(t, s.backend, s.gasPriceManager, s.admin, testContractAddr)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := testContract.SetGasPriceConfig(s.admin, tt.config)
			require.ErrorContains(t, err, vm.ErrExecutionReverted.Error(), "SetGasPriceConfig(%+v)", tt.config) //nolint:forbidigo // upstream error wrapped as string
		})
	}
}

func TestGasPriceManager_Events(t *testing.T) {
	s := newSUT(t)
	testContractAddr, testContract := s.deployTestContract(t)
	allowlisttest.SetAsEnabled(t, s.backend, s.gasPriceManager, s.admin, testContractAddr)

	s.setGasPriceConfig(t, testContract, updatedGasPriceConfig)

	// The event sender is the wrapper contract address, not adminAddress.
	iter, err := s.gasPriceManager.FilterGasPriceConfigUpdated(nil, []common.Address{testContractAddr})
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
