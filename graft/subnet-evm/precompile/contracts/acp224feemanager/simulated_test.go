// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// simulated_test.go validates the ACP-224 fee manager precompile through its
// generated Go bindings, exercising the full
// Solidity interface -> ABI encoding -> precompile -> ABI decoding -> Go
// binding path on a simulated backend. Some tests overlap with
// contract_test.go (e.g. authorization, fee config validation) but the overlap
// is intentional. contract_test.go tests the precompile logic directly, while
// these tests verify the ABI round-trip.
//
// Errors from the precompile lose their identity when serialized as EVM revert
// data, so error assertions use ErrorContains instead of ErrorIs.

package acp224feemanager_test

import (
	"crypto/ecdsa"
	"testing"

	"github.com/ava-labs/libevm/common"
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
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/utilstest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/modules"
	"github.com/ava-labs/avalanchego/utils"

	sim "github.com/ava-labs/avalanchego/graft/subnet-evm/ethclient/simulated"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/acp224feemanager/acp224feemanagertest/bindings"
)

var (
	adminKey, _        = crypto.GenerateKey()
	enabledKey, _      = crypto.GenerateKey()
	managerKey, _      = crypto.GenerateKey()
	unprivilegedKey, _ = crypto.GenerateKey()

	adminAddress        = crypto.PubkeyToAddress(adminKey.PublicKey)
	enabledAddress      = crypto.PubkeyToAddress(enabledKey.PublicKey)
	managerAddress      = crypto.PubkeyToAddress(managerKey.PublicKey)
	unprivilegedAddress = crypto.PubkeyToAddress(unprivilegedKey.PublicKey)

	fundedAddresses = []common.Address{adminAddress, enabledAddress, managerAddress, unprivilegedAddress}
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

func setFeeConfig(t *testing.T, b *sim.Backend, contract *bindings.IACP224FeeManager, auth *bind.TransactOpts, config commontype.ACP224FeeConfig) uint64 {
	t.Helper()
	tx, err := contract.SetFeeConfig(auth, toBindingsFeeConfig(config))
	require.NoError(t, err, "SetFeeConfig()")
	receipt := utilstest.WaitReceiptSuccessful(t, b, tx)
	return receipt.BlockNumber.Uint64()
}

func newPrecompileCfg() *acp224feemanager.Config {
	return acp224feemanager.NewConfig(
		utils.PointerTo[uint64](0),
		[]common.Address{adminAddress},
		[]common.Address{enabledAddress},
		[]common.Address{managerAddress},
		&genesisFeeConfig,
	)
}

func newFeeManagerBackend(t *testing.T, cfg *acp224feemanager.Config, funded []common.Address) (*sim.Backend, *bindings.IACP224FeeManager) {
	t.Helper()
	backend := utilstest.NewBackendWithPrecompile(t, cfg, funded)
	t.Cleanup(func() { backend.Close() })
	feeManager, err := bindings.NewIACP224FeeManager(acp224feemanager.ContractAddress, backend.Client())
	require.NoError(t, err, "NewIACP224FeeManager()")
	return backend, feeManager
}

func TestACP224FeeManager_AdminHasAdminRole(t *testing.T) {
	_, feeManager := newFeeManagerBackend(t, newPrecompileCfg(), fundedAddresses)
	allowlisttest.VerifyRole(t, feeManager, adminAddress, allowlist.AdminRole)
}

func TestACP224FeeManager_GrantEnabledRole(t *testing.T) {
	backend, feeManager := newFeeManagerBackend(t, newPrecompileCfg(), fundedAddresses)
	admin := utilstest.NewAuth(t, adminKey, params.TestChainConfig.ChainID)

	newAddr := common.HexToAddress("0x706F6C79746F706961")
	allowlisttest.VerifyRole(t, feeManager, newAddr, allowlist.NoRole)
	allowlisttest.SetAsEnabled(t, backend, feeManager, admin, newAddr)
	allowlisttest.VerifyRole(t, feeManager, newAddr, allowlist.EnabledRole)
}

func TestACP224FeeManager_SetFeeConfigAuthorization(t *testing.T) {
	tests := []struct {
		name    string
		key     *ecdsa.PrivateKey
		wantErr string
	}{
		{
			name:    "NoRole rejected",
			key:     unprivilegedKey,
			wantErr: acp224feemanager.ErrCannotSetFeeConfig.Error(),
		},
		{
			name: "Enabled succeeds",
			key:  enabledKey,
		},
		{
			name: "Manager succeeds",
			key:  managerKey,
		},
		{
			name: "Admin succeeds",
			key:  adminKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend, feeManager := newFeeManagerBackend(t, newPrecompileCfg(), fundedAddresses)

			auth := utilstest.NewAuth(t, tt.key, params.TestChainConfig.ChainID)
			tx, err := feeManager.SetFeeConfig(auth, toBindingsFeeConfig(updatedFeeConfig))
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr) //nolint:forbidigo
				return
			}
			require.NoError(t, err, "SetFeeConfig()")
			utilstest.WaitReceiptSuccessful(t, backend, tx)

			got, err := feeManager.GetFeeConfig(nil)
			require.NoError(t, err, "GetFeeConfig()")
			require.Equal(t, updatedFeeConfig, toCommonFeeConfig(got), "fee config after set")
		})
	}
}

func TestACP224FeeManager_SetAndReadFeeConfig(t *testing.T) {
	backend, feeManager := newFeeManagerBackend(t, newPrecompileCfg(), fundedAddresses)
	admin := utilstest.NewAuth(t, adminKey, params.TestChainConfig.ChainID)

	got, err := feeManager.GetFeeConfig(nil)
	require.NoError(t, err, "GetFeeConfig() before set")
	require.Equal(t, genesisFeeConfig, toCommonFeeConfig(got), "genesis fee config")

	blockNum := setFeeConfig(t, backend, feeManager, admin, updatedFeeConfig)

	got, err = feeManager.GetFeeConfig(nil)
	require.NoError(t, err, "GetFeeConfig() after set")
	require.Equal(t, updatedFeeConfig, toCommonFeeConfig(got), "updated fee config")

	lastChangedAt, err := feeManager.GetFeeConfigLastChangedAt(nil)
	require.NoError(t, err, "GetFeeConfigLastChangedAt()")
	require.Equal(t, blockNum, lastChangedAt.Uint64(), "last changed at block number")
}

func TestACP224FeeManager_BoolFieldsRoundTrip(t *testing.T) {
	backend, feeManager := newFeeManagerBackend(t, newPrecompileCfg(), fundedAddresses)
	admin := utilstest.NewAuth(t, adminKey, params.TestChainConfig.ChainID)

	setFeeConfig(t, backend, feeManager, admin, boolFeeConfig)

	got, err := feeManager.GetFeeConfig(nil)
	require.NoError(t, err, "GetFeeConfig()")
	require.Equal(t, boolFeeConfig, toCommonFeeConfig(got), "boolean fee config round-trip")
}

func TestACP224FeeManager_Revocation(t *testing.T) {
	backend, feeManager := newFeeManagerBackend(t, newPrecompileCfg(), fundedAddresses)

	admin := utilstest.NewAuth(t, adminKey, params.TestChainConfig.ChainID)
	enabled := utilstest.NewAuth(t, enabledKey, params.TestChainConfig.ChainID)

	// Enabled can set config before revocation.
	setFeeConfig(t, backend, feeManager, enabled, updatedFeeConfig)

	got, err := feeManager.GetFeeConfig(nil)
	require.NoError(t, err, "GetFeeConfig() after enabled set")
	require.Equal(t, updatedFeeConfig, toCommonFeeConfig(got), "config set by enabled")

	// Admin revokes enabled to NoRole.
	allowlisttest.SetAsNone(t, backend, feeManager, admin, enabledAddress)
	allowlisttest.VerifyRole(t, feeManager, enabledAddress, allowlist.NoRole)

	// Enabled can no longer set config.
	_, err = feeManager.SetFeeConfig(enabled, toBindingsFeeConfig(boolFeeConfig))
	require.ErrorContains(t, err, acp224feemanager.ErrCannotSetFeeConfig.Error()) //nolint:forbidigo
}

func TestACP224FeeManager_InvalidFeeConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  bindings.IACP224FeeManagerFeeConfig
		wantErr string
	}{
		{
			name: "zero MinGasPrice",
			config: bindings.IACP224FeeManagerFeeConfig{
				TargetGas:    1_000_000,
				TimeToDouble: 60,
			},
			wantErr: commontype.ErrMinGasPriceTooLow.Error(),
		},
		{
			name: "ValidatorTargetGas with non-zero TargetGas",
			config: bindings.IACP224FeeManagerFeeConfig{
				ValidatorTargetGas: true,
				TargetGas:          1_000_000,
				MinGasPrice:        1,
			},
			wantErr: commontype.ErrTargetGasMustBeZero.Error(),
		},
		{
			name: "StaticPricing with non-zero TimeToDouble",
			config: bindings.IACP224FeeManagerFeeConfig{
				StaticPricing: true,
				TargetGas:     1_000_000,
				MinGasPrice:   1,
				TimeToDouble:  60,
			},
			wantErr: commontype.ErrTimeToDoubleMustBeZero.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, feeManager := newFeeManagerBackend(t, newPrecompileCfg(), fundedAddresses)
			admin := utilstest.NewAuth(t, adminKey, params.TestChainConfig.ChainID)

			_, err := feeManager.SetFeeConfig(admin, tt.config)
			require.ErrorContains(t, err, tt.wantErr) //nolint:forbidigo
		})
	}
}

func TestACP224FeeManager_Events(t *testing.T) {
	admin := utilstest.NewAuth(t, adminKey, params.TestChainConfig.ChainID)

	precompileCfg := acp224feemanager.NewConfig(
		utils.PointerTo[uint64](0),
		[]common.Address{adminAddress},
		nil, nil,
		&genesisFeeConfig,
	)
	backend, feeManager := newFeeManagerBackend(t, precompileCfg, []common.Address{adminAddress})

	setFeeConfig(t, backend, feeManager, admin, updatedFeeConfig)

	iter, err := feeManager.FilterFeeConfigUpdated(nil, []common.Address{adminAddress})
	require.NoError(t, err, "FilterFeeConfigUpdated()")
	defer iter.Close()

	require.True(t, iter.Next(), "expected FeeConfigUpdated event")
	event := iter.Event
	require.Equal(t, adminAddress, event.Sender, "event sender")
	require.Equal(t, genesisFeeConfig, toCommonFeeConfig(event.OldFeeConfig), "old fee config in event")
	require.Equal(t, updatedFeeConfig, toCommonFeeConfig(event.NewFeeConfig), "new fee config in event")
	require.False(t, iter.Next(), "expected no more events")
	require.NoError(t, iter.Error(), "iterator error")
}
