// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp224feemanager

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
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompiletest"
)

var (
	testFeeConfig = commontype.ACP224FeeConfig{
		TargetGas:    10_000_000,
		MinGasPrice:  1,
		TimeToDouble: 60,
	}

	// testBoolFeeConfig exercises boolean field round-trip through storage
	// (ValidatorTargetGas=true, StaticPricing=true).
	testBoolFeeConfig = commontype.ACP224FeeConfig{
		ValidatorTargetGas: true,
		StaticPricing:      true,
		MinGasPrice:        1,
	}

	testBlockNumber = big.NewInt(7)

	// defaultConfig sets up roles via Configure, which also stores
	// DefaultACP224FeeConfig to contract storage.
	defaultConfig = &Config{
		AllowListConfig: allowlist.AllowListConfig{
			AdminAddresses:   []common.Address{allowlisttest.TestAdminAddr},
			ManagerAddresses: []common.Address{allowlisttest.TestManagerAddr},
			EnabledAddresses: []common.Address{allowlisttest.TestEnabledAddr},
		},
	}
)

func mustPackGetFeeConfigInput(t testing.TB) []byte {
	t.Helper()
	input, err := PackGetFeeConfig()
	require.NoError(t, err, "PackGetFeeConfig()")
	return input
}

func mustPackGetFeeConfigLastChangedAtInput(t testing.TB) []byte {
	t.Helper()
	input, err := PackGetFeeConfigLastChangedAt()
	require.NoError(t, err, "PackGetFeeConfigLastChangedAt()")
	return input
}

func mustPackSetFeeConfigInput(t testing.TB, config commontype.ACP224FeeConfig) []byte {
	t.Helper()
	input, err := PackSetFeeConfig(config)
	require.NoError(t, err, "PackSetFeeConfig()")
	return input
}

func mustStoreTestFeeConfig(t testing.TB, state *extstate.StateDB) {
	t.Helper()
	require.NoError(t, StoreFeeConfig(state, ContractAddress, testFeeConfig, testBlockNumber), "StoreFeeConfig()")
}

func mustPackGetFeeConfigOutput(t testing.TB, config commontype.ACP224FeeConfig) []byte {
	t.Helper()
	res, err := PackGetFeeConfigOutput(config)
	require.NoError(t, err, "PackGetFeeConfigOutput()")
	return res
}

func mustPackGetFeeConfigLastChangedAtOutput(t testing.TB, blockNumber *big.Int) []byte {
	t.Helper()
	res, err := PackGetFeeConfigLastChangedAtOutput(blockNumber)
	require.NoError(t, err, "PackGetFeeConfigLastChangedAtOutput()")
	return res
}

func TestACP224FeeManagerRun(t *testing.T) {
	tests := []precompiletest.PrecompileTest{
		// getFeeConfig — all roles can read
		{
			Name:        "getFeeConfig_from_NoRole",
			Caller:      allowlisttest.TestNoRoleAddr,
			Config:      defaultConfig,
			InputFn:     mustPackGetFeeConfigInput,
			SuppliedGas: getFeeConfigGasCost,
			ExpectedRes: mustPackGetFeeConfigOutput(t, commontype.DefaultACP224FeeConfig()),
		},
		{
			Name:        "getFeeConfig_from_Enabled",
			Caller:      allowlisttest.TestEnabledAddr,
			Config:      defaultConfig,
			InputFn:     mustPackGetFeeConfigInput,
			SuppliedGas: getFeeConfigGasCost,
			ExpectedRes: mustPackGetFeeConfigOutput(t, commontype.DefaultACP224FeeConfig()),
		},
		{
			Name:        "getFeeConfig_from_Manager",
			Caller:      allowlisttest.TestManagerAddr,
			Config:      defaultConfig,
			InputFn:     mustPackGetFeeConfigInput,
			SuppliedGas: getFeeConfigGasCost,
			ExpectedRes: mustPackGetFeeConfigOutput(t, commontype.DefaultACP224FeeConfig()),
		},
		{
			Name:        "getFeeConfig_from_Admin",
			Caller:      allowlisttest.TestAdminAddr,
			Config:      defaultConfig,
			InputFn:     mustPackGetFeeConfigInput,
			SuppliedGas: getFeeConfigGasCost,
			ExpectedRes: mustPackGetFeeConfigOutput(t, commontype.DefaultACP224FeeConfig()),
		},
		{
			Name:    "getFeeConfig_returns_initialFeeConfig_from_configure",
			Caller:  allowlisttest.TestNoRoleAddr,
			InputFn: mustPackGetFeeConfigInput,
			Config: &Config{
				InitialFeeConfig: &testFeeConfig,
			},
			SuppliedGas: getFeeConfigGasCost,
			ExpectedRes: mustPackGetFeeConfigOutput(t, testFeeConfig),
		},
		{
			Name:        "getFeeConfig_after_store_returns_new_config",
			Caller:      allowlisttest.TestEnabledAddr,
			BeforeHook:  mustStoreTestFeeConfig,
			InputFn:     mustPackGetFeeConfigInput,
			SuppliedGas: getFeeConfigGasCost,
			ExpectedRes: mustPackGetFeeConfigOutput(t, testFeeConfig),
		},
		{
			Name:   "getFeeConfig_boolean_fields_round_trip",
			Caller: allowlisttest.TestEnabledAddr,
			BeforeHook: func(t testing.TB, state *extstate.StateDB) {
				t.Helper()
				require.NoError(t, StoreFeeConfig(state, ContractAddress, testBoolFeeConfig, testBlockNumber), "StoreFeeConfig()")
			},
			InputFn:     mustPackGetFeeConfigInput,
			SuppliedGas: getFeeConfigGasCost,
			ExpectedRes: mustPackGetFeeConfigOutput(t, testBoolFeeConfig),
		},
		{
			Name:        "getFeeConfig_insufficient_gas",
			Caller:      allowlisttest.TestNoRoleAddr,
			InputFn:     mustPackGetFeeConfigInput,
			SuppliedGas: getFeeConfigGasCost - 1,
			ExpectedErr: vm.ErrOutOfGas,
		},

		// getFeeConfigLastChangedAt — all roles can read
		{
			Name:        "getFeeConfigLastChangedAt_from_NoRole",
			Caller:      allowlisttest.TestNoRoleAddr,
			BeforeHook:  mustStoreTestFeeConfig,
			InputFn:     mustPackGetFeeConfigLastChangedAtInput,
			SuppliedGas: getFeeConfigLastChangedAtGasCost,
			ExpectedRes: mustPackGetFeeConfigLastChangedAtOutput(t, testBlockNumber),
		},
		{
			Name:        "getFeeConfigLastChangedAt_from_Enabled",
			Caller:      allowlisttest.TestEnabledAddr,
			BeforeHook:  mustStoreTestFeeConfig,
			InputFn:     mustPackGetFeeConfigLastChangedAtInput,
			SuppliedGas: getFeeConfigLastChangedAtGasCost,
			ExpectedRes: mustPackGetFeeConfigLastChangedAtOutput(t, testBlockNumber),
		},
		{
			Name:        "getFeeConfigLastChangedAt_from_Manager",
			Caller:      allowlisttest.TestManagerAddr,
			BeforeHook:  mustStoreTestFeeConfig,
			InputFn:     mustPackGetFeeConfigLastChangedAtInput,
			SuppliedGas: getFeeConfigLastChangedAtGasCost,
			ExpectedRes: mustPackGetFeeConfigLastChangedAtOutput(t, testBlockNumber),
		},
		{
			Name:        "getFeeConfigLastChangedAt_from_Admin",
			Caller:      allowlisttest.TestAdminAddr,
			BeforeHook:  mustStoreTestFeeConfig,
			InputFn:     mustPackGetFeeConfigLastChangedAtInput,
			SuppliedGas: getFeeConfigLastChangedAtGasCost,
			ExpectedRes: mustPackGetFeeConfigLastChangedAtOutput(t, testBlockNumber),
		},
		{
			Name:        "getFeeConfigLastChangedAt_insufficient_gas",
			Caller:      allowlisttest.TestNoRoleAddr,
			InputFn:     mustPackGetFeeConfigLastChangedAtInput,
			SuppliedGas: getFeeConfigLastChangedAtGasCost - 1,
			ExpectedErr: vm.ErrOutOfGas,
		},

		// setFeeConfig — NoRole rejected, Enabled/Manager/Admin succeed
		{
			Name:   "setFeeConfig_from_NoRole_rejected",
			Caller: allowlisttest.TestNoRoleAddr,
			Config: defaultConfig,
			InputFn: func(t testing.TB) []byte {
				return mustPackSetFeeConfigInput(t, testFeeConfig)
			},
			SuppliedGas: setFeeConfigGasCost,
			ExpectedErr: errCannotSetFeeConfig,
		},
		{
			Name:   "setFeeConfig_from_Enabled",
			Caller: allowlisttest.TestEnabledAddr,
			Config: defaultConfig,
			InputFn: func(t testing.TB) []byte {
				return mustPackSetFeeConfigInput(t, testFeeConfig)
			},
			SuppliedGas: setFeeConfigGasCost,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				got := GetStoredFeeConfig(state, ContractAddress)
				require.Equal(t, testFeeConfig, got, "GetStoredFeeConfig()")
			},
		},
		{
			Name:   "setFeeConfig_from_Manager",
			Caller: allowlisttest.TestManagerAddr,
			Config: defaultConfig,
			InputFn: func(t testing.TB) []byte {
				return mustPackSetFeeConfigInput(t, testFeeConfig)
			},
			SuppliedGas: setFeeConfigGasCost,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				got := GetStoredFeeConfig(state, ContractAddress)
				require.Equal(t, testFeeConfig, got, "GetStoredFeeConfig()")
			},
		},
		{
			Name:   "setFeeConfig_from_Admin",
			Caller: allowlisttest.TestAdminAddr,
			Config: defaultConfig,
			InputFn: func(t testing.TB) []byte {
				return mustPackSetFeeConfigInput(t, testFeeConfig)
			},
			SuppliedGas: setFeeConfigGasCost,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				got := GetStoredFeeConfig(state, ContractAddress)
				require.Equal(t, testFeeConfig, got, "GetStoredFeeConfig()")
			},
		},
		{
			Name:   "setFeeConfig_readOnly_rejected",
			Caller: allowlisttest.TestEnabledAddr,
			Config: defaultConfig,
			InputFn: func(t testing.TB) []byte {
				return mustPackSetFeeConfigInput(t, testFeeConfig)
			},
			SuppliedGas: setFeeConfigGasCost,
			ReadOnly:    true,
			ExpectedErr: vm.ErrWriteProtection,
		},
		{
			Name:   "setFeeConfig_insufficient_gas",
			Caller: allowlisttest.TestEnabledAddr,
			InputFn: func(t testing.TB) []byte {
				return mustPackSetFeeConfigInput(t, testFeeConfig)
			},
			SuppliedGas: setFeeConfigGasCost - 1,
			ExpectedErr: vm.ErrOutOfGas,
		},
		{
			Name:   "setFeeConfig_nil_block_number",
			Caller: allowlisttest.TestEnabledAddr,
			BeforeHook: func(t testing.TB, state *extstate.StateDB) {
				t.Helper()
				allowlisttest.SetDefaultRoles(Module.Address)(t, state)
				require.NoError(t, StoreFeeConfig(state, ContractAddress, commontype.DefaultACP224FeeConfig(), big.NewInt(0)), "StoreFeeConfig()")
			},
			InputFn: func(t testing.TB) []byte {
				return mustPackSetFeeConfigInput(t, testFeeConfig)
			},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().Number().Return((*big.Int)(nil)).AnyTimes()
				mbc.EXPECT().Timestamp().Return(uint64(0)).AnyTimes()
			},
			SuppliedGas: setFeeConfigGasCost,
			ExpectedErr: errNilBlockNumber,
		},
		{
			Name:   "setFeeConfig_invalid_config",
			Caller: allowlisttest.TestEnabledAddr,
			Config: defaultConfig,
			InputFn: func(t testing.TB) []byte {
				return mustPackSetFeeConfigInput(t, commontype.ACP224FeeConfig{
					TargetGas:    commontype.MinTargetGasACP224,
					TimeToDouble: 60,
				})
			},
			SuppliedGas: setFeeConfigGasCost,
			ExpectedErr: commontype.ErrMinGasPriceTooLow,
		},
		{
			Name:   "setFeeConfig_emits_event",
			Caller: allowlisttest.TestEnabledAddr,
			Config: defaultConfig,
			InputFn: func(t testing.TB) []byte {
				return mustPackSetFeeConfigInput(t, testFeeConfig)
			},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().Number().Return(testBlockNumber).AnyTimes()
				mbc.EXPECT().Timestamp().Return(uint64(0)).AnyTimes()
			},
			SuppliedGas: setFeeConfigGasCost,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				feeConfig := GetStoredFeeConfig(state, ContractAddress)
				require.Equal(t, testFeeConfig, feeConfig, "GetStoredFeeConfig()")

				lastChangedAt := GetFeeConfigLastChangedAt(state, ContractAddress)
				require.Equal(t, testBlockNumber, lastChangedAt, "GetFeeConfigLastChangedAt()")

				logs := state.Logs()
				require.Len(t, logs, 1, "logs emitted")
				log := logs[0]
				require.Equal(t, ContractAddress, log.Address, "log address")

				require.Len(t, log.Topics, 2, "topics (event sig + indexed sender)")
				wantTopics, _, err := PackFeeConfigUpdatedEvent(
					allowlisttest.TestEnabledAddr,
					commontype.DefaultACP224FeeConfig(),
					testFeeConfig,
				)
				require.NoError(t, err, "PackFeeConfigUpdatedEvent()")
				require.Equal(t, wantTopics, log.Topics, "event topics")

				unpacked, err := UnpackFeeConfigUpdatedEventData(log.Data)
				require.NoError(t, err, "UnpackFeeConfigUpdatedEventData()")
				require.Equal(t, commontype.DefaultACP224FeeConfig(), unpacked.OldFeeConfig, "old fee config in event")
				require.Equal(t, testFeeConfig, unpacked.NewFeeConfig, "new fee config in event")
			},
		},
	}

	precompiletest.RunPrecompileTests(t, Module, tests)
}

func TestUnpackSetFeeConfigInput_malformed(t *testing.T) {
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
			_, err := UnpackSetFeeConfigInput(tt.input)
			//nolint:forbidigo // ABI decode errors are unexported; ErrorIs is not possible
			require.ErrorContains(t, err, tt.wantErr, "UnpackSetFeeConfigInput(%x)", tt.input)
		})
	}
}
