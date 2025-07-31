// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package feemanager

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	ethtypes "github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/core/extstate"
	"github.com/ava-labs/subnet-evm/precompile/allowlist/allowlisttest"
	"github.com/ava-labs/subnet-evm/precompile/contract"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/subnet-evm/precompile/precompiletest"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
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
	tests           = map[string]precompiletest.PrecompileTest{
		"set config from no role fails": {
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
		"set config from enabled address succeeds and emits logs": {
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
		"set config from manager succeeds": {
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
		"set invalid config from enabled address": {
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
		"set config from admin address": {
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
				require.EqualValues(t, testBlockNumber, lastChangedAt)

				logs := state.Logs()
				assertFeeEvent(t, logs, allowlisttest.TestAdminAddr, zeroFeeConfig, testFeeConfig)
			},
		},
		"get fee config from non-enabled address": {
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
				require.EqualValues(t, big.NewInt(6), lastChangedAt)
			},
		},
		"get initial fee config": {
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
				require.EqualValues(t, testBlockNumber, lastChangedAt)
			},
		},
		"get last changed at from non-enabled address": {
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
		"readOnly setFeeConfig with noRole fails": {
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
		"readOnly setFeeConfig with allow role fails": {
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
		"readOnly setFeeConfig with admin role fails": {
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
		"insufficient gas setFeeConfig from admin": {
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
		"set config with extra padded bytes should fail before Durango": {
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
		"set config with extra padded bytes should succeed with Durango": {
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
				require.EqualValues(t, testBlockNumber, lastChangedAt)

				logs := state.Logs()
				assertFeeEvent(t, logs, allowlisttest.TestEnabledAddr, zeroFeeConfig, testFeeConfig)
			},
		},
		// from https://github.com/ava-labs/subnet-evm/issues/487
		"setFeeConfig regression test should fail before Durango": {
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
		"setFeeConfig regression test should succeed after Durango": {
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
				require.EqualValues(t, testBlockNumber, lastChangedAt)

				logs := state.Logs()
				assertFeeEvent(t, logs, allowlisttest.TestEnabledAddr, zeroFeeConfig, regressionFeeConfig)
			},
		},
		"set config should not emit event before Durango": {
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
