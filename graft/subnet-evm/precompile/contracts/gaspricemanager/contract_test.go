// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gaspricemanager_test

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core/extstate"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist/allowlisttest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contract"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/gaspricemanager"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompiletest"
)

var (
	testGasPriceConfig = commontype.GasPriceConfig{
		TargetGas:    10_000_000,
		MinGasPrice:  1,
		TimeToDouble: 60,
	}

	// testBoolGasPriceConfig exercises boolean field round-trip through storage
	// (ValidatorTargetGas=true, StaticPricing=true).
	testBoolGasPriceConfig = commontype.GasPriceConfig{
		ValidatorTargetGas: true,
		StaticPricing:      true,
		MinGasPrice:        1,
	}

	testBlockNumber = big.NewInt(7)

	// defaultConfig sets up roles via Configure, which also stores
	// DefaultGasPriceConfig to contract storage.
	defaultConfig = &gaspricemanager.Config{
		AllowListConfig: allowlist.AllowListConfig{
			AdminAddresses:   []common.Address{allowlisttest.TestAdminAddr},
			ManagerAddresses: []common.Address{allowlisttest.TestManagerAddr},
			EnabledAddresses: []common.Address{allowlisttest.TestEnabledAddr},
		},
	}
)

func mustPackGetGasPriceConfigInput(t testing.TB) []byte {
	t.Helper()
	input, err := gaspricemanager.PackGetGasPriceConfig()
	require.NoError(t, err, "PackGetGasPriceConfig()")
	return input
}

func mustPackGetGasPriceConfigLastChangedAtInput(t testing.TB) []byte {
	t.Helper()
	input, err := gaspricemanager.PackGetGasPriceConfigLastChangedAt()
	require.NoError(t, err, "PackGetGasPriceConfigLastChangedAt()")
	return input
}

func mustPackSetGasPriceConfigInput(t testing.TB, config commontype.GasPriceConfig) []byte {
	t.Helper()
	input, err := gaspricemanager.PackSetGasPriceConfig(config)
	require.NoError(t, err, "PackSetGasPriceConfig()")
	return input
}

func mustStoreTestGasPriceConfig(t testing.TB, state *extstate.StateDB) {
	t.Helper()
	require.NoError(t, gaspricemanager.StoreGasPriceConfig(state, gaspricemanager.ContractAddress, testGasPriceConfig, testBlockNumber), "StoreGasPriceConfig()")
}

func mustPackGetGasPriceConfigOutput(t testing.TB, config commontype.GasPriceConfig) []byte {
	t.Helper()
	res, err := gaspricemanager.PackGetGasPriceConfigOutput(config)
	require.NoError(t, err, "PackGetGasPriceConfigOutput()")
	return res
}

func mustPackGetGasPriceConfigLastChangedAtOutput(t testing.TB, blockNumber *big.Int) []byte {
	t.Helper()
	res, err := gaspricemanager.PackGetGasPriceConfigLastChangedAtOutput(blockNumber)
	require.NoError(t, err, "PackGetGasPriceConfigLastChangedAtOutput()")
	return res
}

func TestGasPriceManagerRun(t *testing.T) {
	tests := []precompiletest.PrecompileTest{
		// getGasPriceConfig — all roles can read
		{
			Name:        "getGasPriceConfig_from_NoRole",
			Caller:      allowlisttest.TestNoRoleAddr,
			Config:      defaultConfig,
			InputFn:     mustPackGetGasPriceConfigInput,
			SuppliedGas: gaspricemanager.GetGasPriceConfigGasCost,
			ExpectedRes: mustPackGetGasPriceConfigOutput(t, commontype.DefaultGasPriceConfig()),
		},
		{
			Name:        "getGasPriceConfig_from_Enabled",
			Caller:      allowlisttest.TestEnabledAddr,
			Config:      defaultConfig,
			InputFn:     mustPackGetGasPriceConfigInput,
			SuppliedGas: gaspricemanager.GetGasPriceConfigGasCost,
			ExpectedRes: mustPackGetGasPriceConfigOutput(t, commontype.DefaultGasPriceConfig()),
		},
		{
			Name:        "getGasPriceConfig_from_Manager",
			Caller:      allowlisttest.TestManagerAddr,
			Config:      defaultConfig,
			InputFn:     mustPackGetGasPriceConfigInput,
			SuppliedGas: gaspricemanager.GetGasPriceConfigGasCost,
			ExpectedRes: mustPackGetGasPriceConfigOutput(t, commontype.DefaultGasPriceConfig()),
		},
		{
			Name:        "getGasPriceConfig_from_Admin",
			Caller:      allowlisttest.TestAdminAddr,
			Config:      defaultConfig,
			InputFn:     mustPackGetGasPriceConfigInput,
			SuppliedGas: gaspricemanager.GetGasPriceConfigGasCost,
			ExpectedRes: mustPackGetGasPriceConfigOutput(t, commontype.DefaultGasPriceConfig()),
		},
		{
			Name:    "getGasPriceConfig_returns_initialGasPriceConfig_from_configure",
			Caller:  allowlisttest.TestNoRoleAddr,
			InputFn: mustPackGetGasPriceConfigInput,
			Config: &gaspricemanager.Config{
				InitialGasPriceConfig: &testGasPriceConfig,
			},
			SuppliedGas: gaspricemanager.GetGasPriceConfigGasCost,
			ExpectedRes: mustPackGetGasPriceConfigOutput(t, testGasPriceConfig),
		},
		{
			Name:        "getGasPriceConfig_after_store_returns_new_config",
			Caller:      allowlisttest.TestEnabledAddr,
			BeforeHook:  mustStoreTestGasPriceConfig,
			InputFn:     mustPackGetGasPriceConfigInput,
			SuppliedGas: gaspricemanager.GetGasPriceConfigGasCost,
			ExpectedRes: mustPackGetGasPriceConfigOutput(t, testGasPriceConfig),
		},
		{
			Name:   "getGasPriceConfig_boolean_fields_round_trip",
			Caller: allowlisttest.TestEnabledAddr,
			BeforeHook: func(t testing.TB, state *extstate.StateDB) {
				t.Helper()
				require.NoError(t, gaspricemanager.StoreGasPriceConfig(state, gaspricemanager.ContractAddress, testBoolGasPriceConfig, testBlockNumber), "StoreGasPriceConfig()")
			},
			InputFn:     mustPackGetGasPriceConfigInput,
			SuppliedGas: gaspricemanager.GetGasPriceConfigGasCost,
			ExpectedRes: mustPackGetGasPriceConfigOutput(t, testBoolGasPriceConfig),
		},
		{
			Name:        "getGasPriceConfig_insufficient_gas",
			Caller:      allowlisttest.TestNoRoleAddr,
			InputFn:     mustPackGetGasPriceConfigInput,
			SuppliedGas: gaspricemanager.GetGasPriceConfigGasCost - 1,
			ExpectedErr: vm.ErrOutOfGas,
		},

		// getGasPriceConfigLastChangedAt — all roles can read
		{
			Name:        "getGasPriceConfigLastChangedAt_from_NoRole",
			Caller:      allowlisttest.TestNoRoleAddr,
			BeforeHook:  mustStoreTestGasPriceConfig,
			InputFn:     mustPackGetGasPriceConfigLastChangedAtInput,
			SuppliedGas: gaspricemanager.GetGasPriceConfigLastChangedAtGasCost,
			ExpectedRes: mustPackGetGasPriceConfigLastChangedAtOutput(t, testBlockNumber),
		},
		{
			Name:        "getGasPriceConfigLastChangedAt_from_Enabled",
			Caller:      allowlisttest.TestEnabledAddr,
			BeforeHook:  mustStoreTestGasPriceConfig,
			InputFn:     mustPackGetGasPriceConfigLastChangedAtInput,
			SuppliedGas: gaspricemanager.GetGasPriceConfigLastChangedAtGasCost,
			ExpectedRes: mustPackGetGasPriceConfigLastChangedAtOutput(t, testBlockNumber),
		},
		{
			Name:        "getGasPriceConfigLastChangedAt_from_Manager",
			Caller:      allowlisttest.TestManagerAddr,
			BeforeHook:  mustStoreTestGasPriceConfig,
			InputFn:     mustPackGetGasPriceConfigLastChangedAtInput,
			SuppliedGas: gaspricemanager.GetGasPriceConfigLastChangedAtGasCost,
			ExpectedRes: mustPackGetGasPriceConfigLastChangedAtOutput(t, testBlockNumber),
		},
		{
			Name:        "getGasPriceConfigLastChangedAt_from_Admin",
			Caller:      allowlisttest.TestAdminAddr,
			BeforeHook:  mustStoreTestGasPriceConfig,
			InputFn:     mustPackGetGasPriceConfigLastChangedAtInput,
			SuppliedGas: gaspricemanager.GetGasPriceConfigLastChangedAtGasCost,
			ExpectedRes: mustPackGetGasPriceConfigLastChangedAtOutput(t, testBlockNumber),
		},
		{
			Name:        "getGasPriceConfigLastChangedAt_insufficient_gas",
			Caller:      allowlisttest.TestNoRoleAddr,
			InputFn:     mustPackGetGasPriceConfigLastChangedAtInput,
			SuppliedGas: gaspricemanager.GetGasPriceConfigLastChangedAtGasCost - 1,
			ExpectedErr: vm.ErrOutOfGas,
		},

		// setGasPriceConfig — NoRole rejected, Enabled/Manager/Admin succeed
		{
			Name:   "setGasPriceConfig_from_NoRole_rejected",
			Caller: allowlisttest.TestNoRoleAddr,
			Config: defaultConfig,
			InputFn: func(t testing.TB) []byte {
				return mustPackSetGasPriceConfigInput(t, testGasPriceConfig)
			},
			SuppliedGas: gaspricemanager.SetGasPriceConfigGasCost,
			ExpectedErr: gaspricemanager.ErrCannotSetGasPriceConfig,
		},
		{
			Name:   "setGasPriceConfig_from_Enabled",
			Caller: allowlisttest.TestEnabledAddr,
			Config: defaultConfig,
			InputFn: func(t testing.TB) []byte {
				return mustPackSetGasPriceConfigInput(t, testGasPriceConfig)
			},
			SuppliedGas: gaspricemanager.SetGasPriceConfigGasCost,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				got := gaspricemanager.GetStoredGasPriceConfig(state, gaspricemanager.ContractAddress)
				require.Equal(t, testGasPriceConfig, got, "GetStoredGasPriceConfig()")
			},
		},
		{
			Name:   "setGasPriceConfig_from_Manager",
			Caller: allowlisttest.TestManagerAddr,
			Config: defaultConfig,
			InputFn: func(t testing.TB) []byte {
				return mustPackSetGasPriceConfigInput(t, testGasPriceConfig)
			},
			SuppliedGas: gaspricemanager.SetGasPriceConfigGasCost,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				got := gaspricemanager.GetStoredGasPriceConfig(state, gaspricemanager.ContractAddress)
				require.Equal(t, testGasPriceConfig, got, "GetStoredGasPriceConfig()")
			},
		},
		{
			Name:   "setGasPriceConfig_from_Admin",
			Caller: allowlisttest.TestAdminAddr,
			Config: defaultConfig,
			InputFn: func(t testing.TB) []byte {
				return mustPackSetGasPriceConfigInput(t, testGasPriceConfig)
			},
			SuppliedGas: gaspricemanager.SetGasPriceConfigGasCost,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				got := gaspricemanager.GetStoredGasPriceConfig(state, gaspricemanager.ContractAddress)
				require.Equal(t, testGasPriceConfig, got, "GetStoredGasPriceConfig()")
			},
		},
		{
			Name:   "setGasPriceConfig_readOnly_rejected",
			Caller: allowlisttest.TestEnabledAddr,
			Config: defaultConfig,
			InputFn: func(t testing.TB) []byte {
				return mustPackSetGasPriceConfigInput(t, testGasPriceConfig)
			},
			SuppliedGas: gaspricemanager.SetGasPriceConfigGasCost,
			ReadOnly:    true,
			ExpectedErr: vm.ErrWriteProtection,
		},
		{
			Name:   "setGasPriceConfig_insufficient_gas",
			Caller: allowlisttest.TestEnabledAddr,
			InputFn: func(t testing.TB) []byte {
				return mustPackSetGasPriceConfigInput(t, testGasPriceConfig)
			},
			SuppliedGas: gaspricemanager.SetGasPriceConfigGasCost - 1,
			ExpectedErr: vm.ErrOutOfGas,
		},
		{
			Name:   "setGasPriceConfig_nil_block_number",
			Caller: allowlisttest.TestEnabledAddr,
			BeforeHook: func(t testing.TB, state *extstate.StateDB) {
				t.Helper()
				allowlisttest.SetDefaultRoles(gaspricemanager.Module.Address)(t, state)
				require.NoError(t, gaspricemanager.StoreGasPriceConfig(state, gaspricemanager.ContractAddress, commontype.DefaultGasPriceConfig(), big.NewInt(0)), "StoreGasPriceConfig()")
			},
			InputFn: func(t testing.TB) []byte {
				return mustPackSetGasPriceConfigInput(t, testGasPriceConfig)
			},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().Number().Return((*big.Int)(nil)).AnyTimes()
				mbc.EXPECT().Timestamp().Return(uint64(0)).AnyTimes()
			},
			SuppliedGas: gaspricemanager.SetGasPriceConfigGasCost,
			ExpectedErr: gaspricemanager.ErrNilBlockNumber,
		},
		{
			Name:   "setGasPriceConfig_invalid_config",
			Caller: allowlisttest.TestEnabledAddr,
			Config: defaultConfig,
			InputFn: func(t testing.TB) []byte {
				return mustPackSetGasPriceConfigInput(t, commontype.GasPriceConfig{
					TargetGas:    commontype.MinTargetGas,
					TimeToDouble: 60,
				})
			},
			SuppliedGas: gaspricemanager.SetGasPriceConfigGasCost,
			ExpectedErr: commontype.ErrMinGasPriceTooLow,
		},
		{
			Name:   "setGasPriceConfig_emits_event",
			Caller: allowlisttest.TestEnabledAddr,
			Config: defaultConfig,
			InputFn: func(t testing.TB) []byte {
				return mustPackSetGasPriceConfigInput(t, testGasPriceConfig)
			},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().Number().Return(testBlockNumber).AnyTimes()
				mbc.EXPECT().Timestamp().Return(uint64(0)).AnyTimes()
			},
			SuppliedGas: gaspricemanager.SetGasPriceConfigGasCost,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				gasPriceConfig := gaspricemanager.GetStoredGasPriceConfig(state, gaspricemanager.ContractAddress)
				require.Equal(t, testGasPriceConfig, gasPriceConfig, "GetStoredGasPriceConfig()")

				lastChangedAt := gaspricemanager.GetGasPriceConfigLastChangedAt(state, gaspricemanager.ContractAddress)
				require.Equal(t, testBlockNumber, lastChangedAt, "GetGasPriceConfigLastChangedAt()")

				logs := state.Logs()
				require.Len(t, logs, 1, "logs emitted")
				log := logs[0]
				require.Equal(t, gaspricemanager.ContractAddress, log.Address, "log address")

				require.Len(t, log.Topics, 2, "topics (event sig + indexed sender)")
				wantTopics, _, err := gaspricemanager.PackGasPriceConfigUpdatedEvent(
					allowlisttest.TestEnabledAddr,
					commontype.DefaultGasPriceConfig(),
					testGasPriceConfig,
				)
				require.NoError(t, err, "PackGasPriceConfigUpdatedEvent()")
				require.Equal(t, wantTopics, log.Topics, "event topics")

				unpacked, err := gaspricemanager.UnpackGasPriceConfigUpdatedEventData(log.Data)
				require.NoError(t, err, "UnpackGasPriceConfigUpdatedEventData()")
				require.Equal(t, commontype.DefaultGasPriceConfig(), unpacked.OldGasPriceConfig, "old gas price config in event")
				require.Equal(t, testGasPriceConfig, unpacked.NewGasPriceConfig, "new gas price config in event")
			},
		},
	}

	allowlisttest.RunPrecompileWithAllowListTests(t, gaspricemanager.Module, tests)
}

func TestUnpackSetGasPriceConfigInput_malformed(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantErr string
	}{
		{
			name:    "nil",
			wantErr: "abi: attempting to unmarshal an empty string while arguments are expected",
		},
		{
			name:    "empty",
			input:   []byte{},
			wantErr: "abi: attempting to unmarshal an empty string while arguments are expected",
		},
		{
			name:    "random",
			input:   []byte("random"),
			wantErr: "abi: cannot marshal in to go type: length insufficient",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := gaspricemanager.UnpackSetGasPriceConfigInput(tt.input)
			//nolint:forbidigo // ABI decode errors are unexported; ErrorIs is not possible
			require.ErrorContains(t, err, tt.wantErr, "UnpackSetGasPriceConfigInput(%x)", tt.input)
		})
	}
}
