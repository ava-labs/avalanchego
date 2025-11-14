// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package feemanager

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/core/extstate"
	"github.com/ava-labs/subnet-evm/precompile/allowlist/allowlisttest"
	"github.com/ava-labs/subnet-evm/precompile/contract"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/subnet-evm/precompile/precompiletest"

	ethtypes "github.com/ava-labs/libevm/core/types"
)

var (
	regressionBytes     = "8f10b58600000000000000000000000000000000000000000000000000000000017d78400000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000012a05f20000000000000000000000000000000000000000000000000000000000047868c0000000000000000000000000000000000000000000000000000000000000005400000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001bc16d674ec800000000000000000000000000000000000000000000000000000de0b6b3a764000000000000000000000000000000000000000000000000000000000000"
	regressionFeeConfig = commontype.FeeConfig{
		GasLimit:                 big.NewInt(25000000),
		TargetBlockRate:          2,
		MinBaseFee:               big.NewInt(5000000000),
		TargetGas:                big.NewInt(75000000),
		BaseFeeChangeDenominator: big.NewInt(84),
		MinBlockGasCost:          big.NewInt(0),
		MaxBlockGasCost:          big.NewInt(2000000000000000000),
		BlockGasCostStep:         big.NewInt(1000000000000000000),
	}
	testFeeConfig = commontype.FeeConfig{
		GasLimit:        big.NewInt(8_000_000),
		TargetBlockRate: 2, // in seconds

		MinBaseFee:               big.NewInt(25_000_000_000),
		TargetGas:                big.NewInt(15_000_000),
		BaseFeeChangeDenominator: big.NewInt(36),

		MinBlockGasCost:  big.NewInt(0),
		MaxBlockGasCost:  big.NewInt(1_000_000),
		BlockGasCostStep: big.NewInt(200_000),
	}
	zeroFeeConfig = commontype.FeeConfig{
		GasLimit:                 new(big.Int),
		MinBaseFee:               new(big.Int),
		TargetGas:                new(big.Int),
		BaseFeeChangeDenominator: new(big.Int),

		MinBlockGasCost:  new(big.Int),
		MaxBlockGasCost:  new(big.Int),
		BlockGasCostStep: new(big.Int),
	}
	testBlockNumber = big.NewInt(7)
	tests           = []precompiletest.PrecompileTest{
		{
			Name:       "set_config_from_no_role_fails",
			Caller:     allowlisttest.TestNoRoleAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetFeeConfig(testFeeConfig)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: SetFeeConfigGasCost,
			ReadOnly:    false,
			ExpectedErr: ErrCannotChangeFee.Error(),
		},
		{
			Name:       "set_config_from_enabled_address_succeeds_and_emits_logs",
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetFeeConfig(testFeeConfig)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: SetFeeConfigGasCost + FeeConfigChangedEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				feeConfig := GetStoredFeeConfig(state)
				require.Equal(t, testFeeConfig, feeConfig)

				logs := state.Logs()
				assertFeeEvent(t, logs, allowlisttest.TestEnabledAddr, zeroFeeConfig, testFeeConfig)
			},
		},
		{
			Name:       "set_config_from_manager_succeeds",
			Caller:     allowlisttest.TestManagerAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetFeeConfig(testFeeConfig)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: SetFeeConfigGasCost + FeeConfigChangedEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				feeConfig := GetStoredFeeConfig(state)
				require.Equal(t, testFeeConfig, feeConfig)

				logs := state.Logs()
				assertFeeEvent(t, logs, allowlisttest.TestManagerAddr, zeroFeeConfig, testFeeConfig)
			},
		},
		{
			Name:       "set_invalid_config_from_enabled_address",
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				feeConfig := testFeeConfig
				feeConfig.MinBlockGasCost = new(big.Int).Mul(feeConfig.MaxBlockGasCost, common.Big2)
				input, err := PackSetFeeConfig(feeConfig)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: SetFeeConfigGasCost + FeeConfigChangedEventGasCost,
			ReadOnly:    false,
			Config: &Config{
				InitialFeeConfig: &testFeeConfig,
			},
			ExpectedErr: "cannot be greater than maxBlockGasCost",
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				feeConfig := GetStoredFeeConfig(state)
				require.Equal(t, testFeeConfig, feeConfig)
			},
		},
		{
			Name:       "set_config_from_admin_address",
			Caller:     allowlisttest.TestAdminAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetFeeConfig(testFeeConfig)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: SetFeeConfigGasCost + FeeConfigChangedEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().Number().Return(testBlockNumber).AnyTimes()
				mbc.EXPECT().Timestamp().Return(uint64(0)).AnyTimes()
			},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				feeConfig := GetStoredFeeConfig(state)
				require.Equal(t, testFeeConfig, feeConfig)
				lastChangedAt := GetFeeConfigLastChangedAt(state)
				require.Equal(t, testBlockNumber, lastChangedAt)

				logs := state.Logs()
				assertFeeEvent(t, logs, allowlisttest.TestAdminAddr, zeroFeeConfig, testFeeConfig)
			},
		},
		{
			Name:   "get_fee_config_from_non_enabled_address",
			Caller: allowlisttest.TestNoRoleAddr,
			BeforeHook: func(t testing.TB, state *extstate.StateDB) {
				blockContext := contract.NewMockBlockContext(gomock.NewController(t))
				blockContext.EXPECT().Number().Return(big.NewInt(6)).Times(1)
				allowlisttest.SetDefaultRoles(Module.Address)(t, state)
				require.NoError(t, StoreFeeConfig(state, testFeeConfig, blockContext))
			},
			InputFn: func(t testing.TB) []byte {
				input, err := PackGetFeeConfig()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: GetFeeConfigGasCost,
			ReadOnly:    true,
			ExpectedRes: func() []byte {
				res, err := PackGetFeeConfigOutput(testFeeConfig)
				if err != nil {
					panic(err)
				}
				return res
			}(),
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				feeConfig := GetStoredFeeConfig(state)
				lastChangedAt := GetFeeConfigLastChangedAt(state)
				require.Equal(t, testFeeConfig, feeConfig)
				require.Equal(t, big.NewInt(6), lastChangedAt)
			},
		},
		{
			Name:       "get_initial_fee_config",
			Caller:     allowlisttest.TestNoRoleAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackGetFeeConfig()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: GetFeeConfigGasCost,
			ReadOnly:    true,
			Config: &Config{
				InitialFeeConfig: &testFeeConfig,
			},
			ExpectedRes: func() []byte {
				res, err := PackGetFeeConfigOutput(testFeeConfig)
				if err != nil {
					panic(err)
				}
				return res
			}(),
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().Number().Return(testBlockNumber)
			},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				feeConfig := GetStoredFeeConfig(state)
				lastChangedAt := GetFeeConfigLastChangedAt(state)
				require.Equal(t, testFeeConfig, feeConfig)
				require.Equal(t, testBlockNumber, lastChangedAt)
			},
		},
		{
			Name:   "get_last_changed_at_from_non_enabled_address",
			Caller: allowlisttest.TestNoRoleAddr,
			BeforeHook: func(t testing.TB, state *extstate.StateDB) {
				blockContext := contract.NewMockBlockContext(gomock.NewController(t))
				blockContext.EXPECT().Number().Return(testBlockNumber).Times(1)
				allowlisttest.SetDefaultRoles(Module.Address)(t, state)
				require.NoError(t, StoreFeeConfig(state, testFeeConfig, blockContext))
			},
			InputFn: func(t testing.TB) []byte {
				input, err := PackGetFeeConfigLastChangedAt()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: GetLastChangedAtGasCost,
			ReadOnly:    true,
			ExpectedRes: func() []byte {
				res, err := PackGetFeeConfigLastChangedAtOutput(testBlockNumber)
				if err != nil {
					panic(err)
				}
				return res
			}(),
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				feeConfig := GetStoredFeeConfig(state)
				lastChangedAt := GetFeeConfigLastChangedAt(state)
				require.Equal(t, testFeeConfig, feeConfig)
				require.Equal(t, testBlockNumber, lastChangedAt)
			},
		},
		{
			Name:       "readOnly_setFeeConfig_with_noRole_fails",
			Caller:     allowlisttest.TestNoRoleAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetFeeConfig(testFeeConfig)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: SetFeeConfigGasCost,
			ReadOnly:    true,
			ExpectedErr: vm.ErrWriteProtection.Error(),
		},
		{
			Name:       "readOnly_setFeeConfig_with_allow_role_fails",
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetFeeConfig(testFeeConfig)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: SetFeeConfigGasCost,
			ReadOnly:    true,
			ExpectedErr: vm.ErrWriteProtection.Error(),
		},
		{
			Name:       "readOnly_setFeeConfig_with_admin_role_fails",
			Caller:     allowlisttest.TestAdminAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetFeeConfig(testFeeConfig)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: SetFeeConfigGasCost,
			ReadOnly:    true,
			ExpectedErr: vm.ErrWriteProtection.Error(),
		},
		{
			Name:       "insufficient_gas_setFeeConfig_from_admin",
			Caller:     allowlisttest.TestAdminAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetFeeConfig(testFeeConfig)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: SetFeeConfigGasCost - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
		{
			Name:       "set_config_with_extra_padded_bytes_should_fail_before_Durango",
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetFeeConfig(testFeeConfig)
				require.NoError(t, err)

				input = append(input, make([]byte, 32)...)
				return input
			},
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(false).AnyTimes()
				return config
			},
			SuppliedGas: SetFeeConfigGasCost,
			ReadOnly:    false,
			ExpectedErr: ErrInvalidLen.Error(),
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().Number().Return(testBlockNumber).AnyTimes()
				mbc.EXPECT().Timestamp().Return(uint64(0)).AnyTimes()
			},
		},
		{
			Name:       "set_config_with_extra_padded_bytes_should_succeed_with_Durango",
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetFeeConfig(testFeeConfig)
				require.NoError(t, err)

				input = append(input, make([]byte, 32)...)
				return input
			},
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(true).AnyTimes()
				return config
			},
			SuppliedGas: SetFeeConfigGasCost + FeeConfigChangedEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().Number().Return(testBlockNumber).AnyTimes()
				mbc.EXPECT().Timestamp().Return(uint64(0)).AnyTimes()
			},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				feeConfig := GetStoredFeeConfig(state)
				require.Equal(t, testFeeConfig, feeConfig)
				lastChangedAt := GetFeeConfigLastChangedAt(state)
				require.Equal(t, testBlockNumber, lastChangedAt)

				logs := state.Logs()
				assertFeeEvent(t, logs, allowlisttest.TestEnabledAddr, zeroFeeConfig, testFeeConfig)
			},
		},
		// from https://github.com/ava-labs/subnet-evm/issues/487
		{
			Name:       "setFeeConfig_regression_test_should_fail_before_Durango",
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			Input:      common.Hex2Bytes(regressionBytes),
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(false).AnyTimes()
				return config
			},
			SuppliedGas: SetFeeConfigGasCost,
			ExpectedErr: ErrInvalidLen.Error(),
			ReadOnly:    false,
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().Number().Return(testBlockNumber).AnyTimes()
				mbc.EXPECT().Timestamp().Return(uint64(0)).AnyTimes()
			},
		},
		{
			Name:       "setFeeConfig_regression_test_should_succeed_after_Durango",
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			Input:      common.Hex2Bytes(regressionBytes),
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(true).AnyTimes()
				return config
			},
			SuppliedGas: SetFeeConfigGasCost + FeeConfigChangedEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().Number().Return(testBlockNumber).AnyTimes()
				mbc.EXPECT().Timestamp().Return(uint64(0)).AnyTimes()
			},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				feeConfig := GetStoredFeeConfig(state)
				require.Equal(t, regressionFeeConfig, feeConfig)
				lastChangedAt := GetFeeConfigLastChangedAt(state)
				require.Equal(t, testBlockNumber, lastChangedAt)

				logs := state.Logs()
				assertFeeEvent(t, logs, allowlisttest.TestEnabledAddr, zeroFeeConfig, regressionFeeConfig)
			},
		},
		{
			Name:       "set_config_should_not_emit_event_before_Durango",
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(false).AnyTimes()
				return config
			},
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetFeeConfig(testFeeConfig)
				require.NoError(t, err)
				return input
			},
			SuppliedGas: SetFeeConfigGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				logs := state.Logs()
				require.Empty(t, logs)
			},
		},
	}
)

func TestFeeManager(t *testing.T) {
	allowlisttest.RunPrecompileWithAllowListTests(t, Module, tests)
}

func assertFeeEvent(
	t testing.TB,
	logs []*ethtypes.Log,
	sender common.Address,
	expectedOldFeeConfig commontype.FeeConfig,
	expectedNewFeeConfig commontype.FeeConfig,
) {
	require.Len(t, logs, 1)
	log := logs[0]
	require.Equal(
		t,
		[]common.Hash{
			FeeManagerABI.Events["FeeConfigChanged"].ID,
			common.BytesToHash(sender[:]),
		},
		log.Topics,
	)

	oldFeeConfig, resFeeConfig, err := UnpackFeeConfigChangedEventData(log.Data)
	require.NoError(t, err)
	require.True(t, expectedOldFeeConfig.Equal(&oldFeeConfig), "expected %v, got %v", expectedOldFeeConfig, oldFeeConfig)
	require.True(t, expectedNewFeeConfig.Equal(&resFeeConfig), "expected %v, got %v", expectedNewFeeConfig, resFeeConfig)
}
