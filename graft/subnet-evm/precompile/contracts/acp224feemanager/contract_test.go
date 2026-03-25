// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp224feemanager_test

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/core/vm"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core/extstate"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist/allowlisttest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contract"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/acp224feemanager"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompiletest"
)

var (
	testFeeConfig = commontype.ACP224FeeConfig{
		TargetGas:    10_000_000,
		MinGasPrice:  1,
		TimeToDouble: 60,
	}

	testBlockNumber = big.NewInt(7)
)

func assertPackGetFeeConfigInput(t testing.TB) []byte {
	t.Helper()
	input, err := acp224feemanager.PackGetFeeConfig()
	require.NoError(t, err, "PackGetFeeConfig()")
	return input
}

func assertPackGetFeeConfigLastChangedAtInput(t testing.TB) []byte {
	t.Helper()
	input, err := acp224feemanager.PackGetFeeConfigLastChangedAt()
	require.NoError(t, err, "PackGetFeeConfigLastChangedAt()")
	return input
}

func assertPackSetFeeConfigInput(t testing.TB, config commontype.ACP224FeeConfig) []byte {
	t.Helper()
	input, err := acp224feemanager.PackSetFeeConfig(config)
	require.NoError(t, err, "PackSetFeeConfig()")
	return input
}

func assertStoreTestFeeConfig(t testing.TB, state *extstate.StateDB) {
	t.Helper()
	require.NoError(t, acp224feemanager.StoreFeeConfig(state, testFeeConfig, testBlockNumber), "StoreFeeConfig()")
}

var tests = []precompiletest.PrecompileTest{
	// getFeeConfig — all roles can read
	{
		Name:        "getFeeConfig_from_NoRole",
		Caller:      allowlisttest.TestNoRoleAddr,
		BeforeHook:  allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address),
		InputFn:     assertPackGetFeeConfigInput,
		SuppliedGas: acp224feemanager.GetFeeConfigGasCost,
		ExpectedRes: mustPackGetFeeConfigOutput(commontype.DefaultACP224FeeConfig),
	},
	{
		Name:        "getFeeConfig_from_Enabled",
		Caller:      allowlisttest.TestEnabledAddr,
		BeforeHook:  allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address),
		InputFn:     assertPackGetFeeConfigInput,
		SuppliedGas: acp224feemanager.GetFeeConfigGasCost,
		ExpectedRes: mustPackGetFeeConfigOutput(commontype.DefaultACP224FeeConfig),
	},
	{
		Name:        "getFeeConfig_from_Manager",
		Caller:      allowlisttest.TestManagerAddr,
		BeforeHook:  allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address),
		InputFn:     assertPackGetFeeConfigInput,
		SuppliedGas: acp224feemanager.GetFeeConfigGasCost,
		ExpectedRes: mustPackGetFeeConfigOutput(commontype.DefaultACP224FeeConfig),
	},
	{
		Name:        "getFeeConfig_from_Admin",
		Caller:      allowlisttest.TestAdminAddr,
		BeforeHook:  allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address),
		InputFn:     assertPackGetFeeConfigInput,
		SuppliedGas: acp224feemanager.GetFeeConfigGasCost,
		ExpectedRes: mustPackGetFeeConfigOutput(commontype.DefaultACP224FeeConfig),
	},
	{
		Name:    "getFeeConfig_returns_initialFeeConfig_from_configure",
		Caller:  allowlisttest.TestNoRoleAddr,
		InputFn: assertPackGetFeeConfigInput,
		Config: &acp224feemanager.Config{
			InitialFeeConfig: &testFeeConfig,
		},
		SuppliedGas: acp224feemanager.GetFeeConfigGasCost,
		ExpectedRes: mustPackGetFeeConfigOutput(testFeeConfig),
	},
	{
		Name:        "getFeeConfig_after_store_returns_new_config",
		Caller:      allowlisttest.TestEnabledAddr,
		BeforeHook:  assertStoreTestFeeConfig,
		InputFn:     assertPackGetFeeConfigInput,
		SuppliedGas: acp224feemanager.GetFeeConfigGasCost,
		ExpectedRes: mustPackGetFeeConfigOutput(testFeeConfig),
	},
	{
		Name:        "getFeeConfig_insufficient_gas",
		Caller:      allowlisttest.TestNoRoleAddr,
		InputFn:     assertPackGetFeeConfigInput,
		SuppliedGas: acp224feemanager.GetFeeConfigGasCost - 1,
		ExpectedErr: vm.ErrOutOfGas,
	},

	// getFeeConfigLastChangedAt — all roles can read
	{
		Name:        "getFeeConfigLastChangedAt_from_NoRole",
		Caller:      allowlisttest.TestNoRoleAddr,
		BeforeHook:  assertStoreTestFeeConfig,
		InputFn:     assertPackGetFeeConfigLastChangedAtInput,
		SuppliedGas: acp224feemanager.GetFeeConfigLastChangedAtGasCost,
		ExpectedRes: mustPackGetFeeConfigLastChangedAtOutput(testBlockNumber),
	},
	{
		Name:        "getFeeConfigLastChangedAt_from_Enabled",
		Caller:      allowlisttest.TestEnabledAddr,
		BeforeHook:  assertStoreTestFeeConfig,
		InputFn:     assertPackGetFeeConfigLastChangedAtInput,
		SuppliedGas: acp224feemanager.GetFeeConfigLastChangedAtGasCost,
		ExpectedRes: mustPackGetFeeConfigLastChangedAtOutput(testBlockNumber),
	},
	{
		Name:        "getFeeConfigLastChangedAt_from_Manager",
		Caller:      allowlisttest.TestManagerAddr,
		BeforeHook:  assertStoreTestFeeConfig,
		InputFn:     assertPackGetFeeConfigLastChangedAtInput,
		SuppliedGas: acp224feemanager.GetFeeConfigLastChangedAtGasCost,
		ExpectedRes: mustPackGetFeeConfigLastChangedAtOutput(testBlockNumber),
	},
	{
		Name:        "getFeeConfigLastChangedAt_from_Admin",
		Caller:      allowlisttest.TestAdminAddr,
		BeforeHook:  assertStoreTestFeeConfig,
		InputFn:     assertPackGetFeeConfigLastChangedAtInput,
		SuppliedGas: acp224feemanager.GetFeeConfigLastChangedAtGasCost,
		ExpectedRes: mustPackGetFeeConfigLastChangedAtOutput(testBlockNumber),
	},
	{
		Name:        "getFeeConfigLastChangedAt_insufficient_gas",
		Caller:      allowlisttest.TestNoRoleAddr,
		InputFn:     assertPackGetFeeConfigLastChangedAtInput,
		SuppliedGas: acp224feemanager.GetFeeConfigLastChangedAtGasCost - 1,
		ExpectedErr: vm.ErrOutOfGas,
	},

	// setFeeConfig — NoRole rejected, Enabled/Manager/Admin succeed
	{
		Name:       "setFeeConfig_from_NoRole_rejected",
		Caller:     allowlisttest.TestNoRoleAddr,
		BeforeHook: allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address),
		InputFn: func(t testing.TB) []byte {
			return assertPackSetFeeConfigInput(t, testFeeConfig)
		},
		SuppliedGas: acp224feemanager.SetFeeConfigGasCost,
		ExpectedErr: acp224feemanager.ErrCannotSetFeeConfig,
	},
	{
		Name:       "setFeeConfig_from_Enabled",
		Caller:     allowlisttest.TestEnabledAddr,
		BeforeHook: allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address),
		InputFn: func(t testing.TB) []byte {
			return assertPackSetFeeConfigInput(t, testFeeConfig)
		},
		SuppliedGas: acp224feemanager.SetFeeConfigGasCost,
		ExpectedRes: []byte{},
	},
	{
		Name:       "setFeeConfig_from_Manager",
		Caller:     allowlisttest.TestManagerAddr,
		BeforeHook: allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address),
		InputFn: func(t testing.TB) []byte {
			return assertPackSetFeeConfigInput(t, testFeeConfig)
		},
		SuppliedGas: acp224feemanager.SetFeeConfigGasCost,
		ExpectedRes: []byte{},
	},
	{
		Name:       "setFeeConfig_from_Admin",
		Caller:     allowlisttest.TestAdminAddr,
		BeforeHook: allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address),
		InputFn: func(t testing.TB) []byte {
			return assertPackSetFeeConfigInput(t, testFeeConfig)
		},
		SuppliedGas: acp224feemanager.SetFeeConfigGasCost,
		ExpectedRes: []byte{},
	},
	{
		Name:       "setFeeConfig_readOnly_rejected",
		Caller:     allowlisttest.TestEnabledAddr,
		BeforeHook: allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address),
		InputFn: func(t testing.TB) []byte {
			return assertPackSetFeeConfigInput(t, testFeeConfig)
		},
		SuppliedGas: acp224feemanager.SetFeeConfigGasCost,
		ReadOnly:    true,
		ExpectedErr: vm.ErrWriteProtection,
	},
	{
		Name:   "setFeeConfig_insufficient_gas",
		Caller: allowlisttest.TestEnabledAddr,
		InputFn: func(t testing.TB) []byte {
			return assertPackSetFeeConfigInput(t, testFeeConfig)
		},
		SuppliedGas: acp224feemanager.SetFeeConfigGasCost - 1,
		ExpectedErr: vm.ErrOutOfGas,
	},
	{
		Name:       "setFeeConfig_nil_block_number",
		Caller:     allowlisttest.TestEnabledAddr,
		BeforeHook: allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address),
		InputFn: func(t testing.TB) []byte {
			return assertPackSetFeeConfigInput(t, testFeeConfig)
		},
		SetupBlockContext: func(mbc *contract.MockBlockContext) {
			mbc.EXPECT().Number().Return((*big.Int)(nil)).AnyTimes()
			mbc.EXPECT().Timestamp().Return(uint64(0)).AnyTimes()
		},
		SuppliedGas: acp224feemanager.SetFeeConfigGasCost,
		ExpectedErr: acp224feemanager.ErrNilBlockNumber,
	},
	{
		Name:       "setFeeConfig_invalid_config",
		Caller:     allowlisttest.TestEnabledAddr,
		BeforeHook: allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address),
		InputFn: func(t testing.TB) []byte {
			return assertPackSetFeeConfigInput(t, commontype.ACP224FeeConfig{
				TargetGas:    commontype.MinTargetGasACP224,
				TimeToDouble: 60,
			})
		},
		SuppliedGas: acp224feemanager.SetFeeConfigGasCost,
		ExpectedErr: commontype.ErrMinGasPriceTooLow,
	},
	{
		Name:       "setFeeConfig_emits_event",
		Caller:     allowlisttest.TestEnabledAddr,
		BeforeHook: allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address),
		InputFn: func(t testing.TB) []byte {
			return assertPackSetFeeConfigInput(t, testFeeConfig)
		},
		SetupBlockContext: func(mbc *contract.MockBlockContext) {
			mbc.EXPECT().Number().Return(testBlockNumber).AnyTimes()
			mbc.EXPECT().Timestamp().Return(uint64(0)).AnyTimes()
		},
		SuppliedGas: acp224feemanager.SetFeeConfigGasCost,
		ExpectedRes: []byte{},
		AfterHook: func(t testing.TB, state *extstate.StateDB) {
			feeConfig := acp224feemanager.GetStoredFeeConfig(state)
			require.Equal(t, testFeeConfig, feeConfig, "GetStoredFeeConfig()")

			lastChangedAt := acp224feemanager.GetFeeConfigLastChangedAt(state)
			require.Equal(t, testBlockNumber, lastChangedAt, "GetFeeConfigLastChangedAt()")

			logs := state.Logs()
			require.Len(t, logs, 1, "logs emitted")
			log := logs[0]
			require.Equal(t, acp224feemanager.ContractAddress, log.Address, "log address")

			require.Len(t, log.Topics, 2, "topics (event sig + indexed sender)")
			wantTopics, _, err := acp224feemanager.PackFeeConfigUpdatedEvent(
				allowlisttest.TestEnabledAddr,
				commontype.DefaultACP224FeeConfig,
				testFeeConfig,
			)
			require.NoError(t, err, "PackFeeConfigUpdatedEvent()")
			require.Equal(t, wantTopics, log.Topics, "event topics")

			unpacked, err := acp224feemanager.UnpackFeeConfigUpdatedEventData(log.Data)
			require.NoError(t, err, "UnpackFeeConfigUpdatedEventData()")
			require.Equal(t, commontype.DefaultACP224FeeConfig, unpacked.OldFeeConfig, "old fee config in event")
			require.Equal(t, testFeeConfig, unpacked.NewFeeConfig, "new fee config in event")
		},
	},
}

func mustPackGetFeeConfigOutput(config commontype.ACP224FeeConfig) []byte {
	res, err := acp224feemanager.PackGetFeeConfigOutput(config)
	if err != nil {
		panic(err)
	}
	return res
}

func mustPackGetFeeConfigLastChangedAtOutput(blockNumber *big.Int) []byte {
	res, err := acp224feemanager.PackGetFeeConfigLastChangedAtOutput(blockNumber)
	if err != nil {
		panic(err)
	}
	return res
}

func TestACP224FeeManagerRun(t *testing.T) {
	precompiletest.RunPrecompileTests(t, acp224feemanager.Module, tests)
}
