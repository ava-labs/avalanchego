// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp224feemanager_test

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core/extstate"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist/allowlisttest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contract"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/acp224feemanager"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompiletest"
)

var (
	testFeeConfig = commontype.ACP224FeeConfig{
		TargetGas:         big.NewInt(10_000_000),
		MinGasPrice:       common.Big1,
		MaxCapacityFactor: big.NewInt(5),
		TimeToDouble:      big.NewInt(60),
	}

	zeroFeeConfig = commontype.ACP224FeeConfig{
		TargetGas:         new(big.Int),
		MinGasPrice:       new(big.Int),
		MaxCapacityFactor: new(big.Int),
		TimeToDouble:      new(big.Int),
	}

	testBlockNumber = big.NewInt(7)

	tests = []precompiletest.PrecompileTest{
		// getFeeConfig tests - all roles should be able to read
		{
			Name:       "calling_getFeeConfig_from_NoRole_should_succeed",
			Caller:     allowlisttest.TestNoRoleAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := acp224feemanager.PackGetFeeConfig()
				require.NoError(t, err)
				return input
			},
			SuppliedGas: acp224feemanager.GetFeeConfigGasCost,
			ReadOnly:    true,
			ExpectedRes: func() []byte {
				res, err := acp224feemanager.PackGetFeeConfigOutput(zeroFeeConfig)
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		{
			Name:       "calling_getFeeConfig_from_Enabled_should_succeed",
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := acp224feemanager.PackGetFeeConfig()
				require.NoError(t, err)
				return input
			},
			SuppliedGas: acp224feemanager.GetFeeConfigGasCost,
			ReadOnly:    true,
			ExpectedRes: func() []byte {
				res, err := acp224feemanager.PackGetFeeConfigOutput(zeroFeeConfig)
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		{
			Name:       "calling_getFeeConfig_from_Manager_should_succeed",
			Caller:     allowlisttest.TestManagerAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := acp224feemanager.PackGetFeeConfig()
				require.NoError(t, err)
				return input
			},
			SuppliedGas: acp224feemanager.GetFeeConfigGasCost,
			ReadOnly:    true,
			ExpectedRes: func() []byte {
				res, err := acp224feemanager.PackGetFeeConfigOutput(zeroFeeConfig)
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		{
			Name:       "calling_getFeeConfig_from_Admin_should_succeed",
			Caller:     allowlisttest.TestAdminAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := acp224feemanager.PackGetFeeConfig()
				require.NoError(t, err)
				return input
			},
			SuppliedGas: acp224feemanager.GetFeeConfigGasCost,
			ReadOnly:    true,
			ExpectedRes: func() []byte {
				res, err := acp224feemanager.PackGetFeeConfigOutput(zeroFeeConfig)
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		{
			Name:   "get_fee_config_after_setting_returns_new_config",
			Caller: allowlisttest.TestEnabledAddr,
			BeforeHook: func(t testing.TB, state *extstate.StateDB) {
				blockContext := contract.NewMockBlockContext(gomock.NewController(t))
				blockContext.EXPECT().Number().Return(testBlockNumber).Times(1)
				allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address)(t, state)
				require.NoError(t, acp224feemanager.StoreFeeConfig(state, acp224feemanager.ContractAddress, testFeeConfig, blockContext))
			},
			InputFn: func(t testing.TB) []byte {
				input, err := acp224feemanager.PackGetFeeConfig()
				require.NoError(t, err)
				return input
			},
			SuppliedGas: acp224feemanager.GetFeeConfigGasCost,
			ReadOnly:    true,
			ExpectedRes: func() []byte {
				res, err := acp224feemanager.PackGetFeeConfigOutput(testFeeConfig)
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		{
			Name:   "insufficient_gas_for_getFeeConfig_should_fail",
			Caller: allowlisttest.TestNoRoleAddr,
			InputFn: func(t testing.TB) []byte {
				input, err := acp224feemanager.PackGetFeeConfig()
				require.NoError(t, err)
				return input
			},
			SuppliedGas: acp224feemanager.GetFeeConfigGasCost - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas,
		},
		// getFeeConfigLastChangedAt tests - all roles should be able to read
		{
			Name:   "calling_getFeeConfigLastChangedAt_from_NoRole_should_succeed",
			Caller: allowlisttest.TestNoRoleAddr,
			BeforeHook: func(t testing.TB, state *extstate.StateDB) {
				blockContext := contract.NewMockBlockContext(gomock.NewController(t))
				blockContext.EXPECT().Number().Return(testBlockNumber).Times(1)
				allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address)(t, state)
				require.NoError(t, acp224feemanager.StoreFeeConfig(state, acp224feemanager.ContractAddress, testFeeConfig, blockContext))
			},
			InputFn: func(t testing.TB) []byte {
				input, err := acp224feemanager.PackGetFeeConfigLastChangedAt()
				require.NoError(t, err)
				return input
			},
			SuppliedGas: acp224feemanager.GetFeeConfigLastChangedAtGasCost,
			ReadOnly:    true,
			ExpectedRes: func() []byte {
				res, err := acp224feemanager.PackGetFeeConfigLastChangedAtOutput(testBlockNumber)
				if err != nil {
					panic(err)
				}
				return res
			}(),
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				feeConfig := acp224feemanager.GetStoredFeeConfig(state, acp224feemanager.ContractAddress)
				require.Equal(t, testFeeConfig, feeConfig)
				lastChangedAt := acp224feemanager.GetFeeConfigLastChangedAt(state, acp224feemanager.ContractAddress)
				require.Equal(t, testBlockNumber, lastChangedAt)
			},
		},
		{
			Name:   "calling_getFeeConfigLastChangedAt_from_Enabled_should_succeed",
			Caller: allowlisttest.TestEnabledAddr,
			BeforeHook: func(t testing.TB, state *extstate.StateDB) {
				blockContext := contract.NewMockBlockContext(gomock.NewController(t))
				blockContext.EXPECT().Number().Return(testBlockNumber).Times(1)
				allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address)(t, state)
				require.NoError(t, acp224feemanager.StoreFeeConfig(state, acp224feemanager.ContractAddress, testFeeConfig, blockContext))
			},
			InputFn: func(t testing.TB) []byte {
				input, err := acp224feemanager.PackGetFeeConfigLastChangedAt()
				require.NoError(t, err)
				return input
			},
			SuppliedGas: acp224feemanager.GetFeeConfigLastChangedAtGasCost,
			ReadOnly:    true,
			ExpectedRes: func() []byte {
				res, err := acp224feemanager.PackGetFeeConfigLastChangedAtOutput(testBlockNumber)
				if err != nil {
					panic(err)
				}
				return res
			}(),
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				feeConfig := acp224feemanager.GetStoredFeeConfig(state, acp224feemanager.ContractAddress)
				require.Equal(t, testFeeConfig, feeConfig)
				lastChangedAt := acp224feemanager.GetFeeConfigLastChangedAt(state, acp224feemanager.ContractAddress)
				require.Equal(t, testBlockNumber, lastChangedAt)
			},
		},
		{
			Name:   "calling_getFeeConfigLastChangedAt_from_Manager_should_succeed",
			Caller: allowlisttest.TestManagerAddr,
			BeforeHook: func(t testing.TB, state *extstate.StateDB) {
				blockContext := contract.NewMockBlockContext(gomock.NewController(t))
				blockContext.EXPECT().Number().Return(testBlockNumber).Times(1)
				allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address)(t, state)
				require.NoError(t, acp224feemanager.StoreFeeConfig(state, acp224feemanager.ContractAddress, testFeeConfig, blockContext))
			},
			InputFn: func(t testing.TB) []byte {
				input, err := acp224feemanager.PackGetFeeConfigLastChangedAt()
				require.NoError(t, err)
				return input
			},
			SuppliedGas: acp224feemanager.GetFeeConfigLastChangedAtGasCost,
			ReadOnly:    true,
			ExpectedRes: func() []byte {
				res, err := acp224feemanager.PackGetFeeConfigLastChangedAtOutput(testBlockNumber)
				if err != nil {
					panic(err)
				}
				return res
			}(),
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				feeConfig := acp224feemanager.GetStoredFeeConfig(state, acp224feemanager.ContractAddress)
				require.Equal(t, testFeeConfig, feeConfig)
				lastChangedAt := acp224feemanager.GetFeeConfigLastChangedAt(state, acp224feemanager.ContractAddress)
				require.Equal(t, testBlockNumber, lastChangedAt)
			},
		},
		{
			Name:   "calling_getFeeConfigLastChangedAt_from_Admin_should_succeed",
			Caller: allowlisttest.TestAdminAddr,
			BeforeHook: func(t testing.TB, state *extstate.StateDB) {
				blockContext := contract.NewMockBlockContext(gomock.NewController(t))
				blockContext.EXPECT().Number().Return(testBlockNumber).Times(1)
				allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address)(t, state)
				require.NoError(t, acp224feemanager.StoreFeeConfig(state, acp224feemanager.ContractAddress, testFeeConfig, blockContext))
			},
			InputFn: func(t testing.TB) []byte {
				input, err := acp224feemanager.PackGetFeeConfigLastChangedAt()
				require.NoError(t, err)
				return input
			},
			SuppliedGas: acp224feemanager.GetFeeConfigLastChangedAtGasCost,
			ReadOnly:    true,
			ExpectedRes: func() []byte {
				res, err := acp224feemanager.PackGetFeeConfigLastChangedAtOutput(testBlockNumber)
				if err != nil {
					panic(err)
				}
				return res
			}(),
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				feeConfig := acp224feemanager.GetStoredFeeConfig(state, acp224feemanager.ContractAddress)
				require.Equal(t, testFeeConfig, feeConfig)
				lastChangedAt := acp224feemanager.GetFeeConfigLastChangedAt(state, acp224feemanager.ContractAddress)
				require.Equal(t, testBlockNumber, lastChangedAt)
			},
		},
		{
			Name:   "insufficient_gas_for_getFeeConfigLastChangedAt_should_fail",
			Caller: allowlisttest.TestNoRoleAddr,
			InputFn: func(t testing.TB) []byte {
				input, err := acp224feemanager.PackGetFeeConfigLastChangedAt()
				require.NoError(t, err)
				return input
			},
			SuppliedGas: acp224feemanager.GetFeeConfigLastChangedAtGasCost - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas,
		},
		// setFeeConfig tests - only Enabled, Manager, and Admin should succeed
		{
			Name:       "calling_setFeeConfig_from_NoRole_should_fail",
			Caller:     allowlisttest.TestNoRoleAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := acp224feemanager.PackSetFeeConfig(testFeeConfig)
				require.NoError(t, err)
				return input
			},
			SuppliedGas: acp224feemanager.SetFeeConfigGasCost,
			ReadOnly:    false,
			ExpectedErr: acp224feemanager.ErrCannotSetFeeConfig,
		},
		{
			Name:       "calling_setFeeConfig_from_Enabled_should_succeed",
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := acp224feemanager.PackSetFeeConfig(testFeeConfig)
				require.NoError(t, err)
				return input
			},
			SuppliedGas: acp224feemanager.SetFeeConfigGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				feeConfig := acp224feemanager.GetStoredFeeConfig(state, acp224feemanager.ContractAddress)
				require.Equal(t, testFeeConfig, feeConfig)
			},
		},
		{
			Name:       "calling_setFeeConfig_from_Manager_should_succeed",
			Caller:     allowlisttest.TestManagerAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := acp224feemanager.PackSetFeeConfig(testFeeConfig)
				require.NoError(t, err)
				return input
			},
			SuppliedGas: acp224feemanager.SetFeeConfigGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				feeConfig := acp224feemanager.GetStoredFeeConfig(state, acp224feemanager.ContractAddress)
				require.Equal(t, testFeeConfig, feeConfig)
			},
		},
		{
			Name:       "calling_setFeeConfig_from_Admin_should_succeed",
			Caller:     allowlisttest.TestAdminAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := acp224feemanager.PackSetFeeConfig(testFeeConfig)
				require.NoError(t, err)
				return input
			},
			SuppliedGas: acp224feemanager.SetFeeConfigGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				feeConfig := acp224feemanager.GetStoredFeeConfig(state, acp224feemanager.ContractAddress)
				require.Equal(t, testFeeConfig, feeConfig)
			},
		},
		{
			Name:   "readOnly_setFeeConfig_should_fail",
			Caller: allowlisttest.TestEnabledAddr,
			InputFn: func(t testing.TB) []byte {
				input, err := acp224feemanager.PackSetFeeConfig(testFeeConfig)
				require.NoError(t, err)
				return input
			},
			SuppliedGas: acp224feemanager.SetFeeConfigGasCost,
			ReadOnly:    true,
			ExpectedErr: vm.ErrWriteProtection,
		},
		{
			Name:   "insufficient_gas_for_setFeeConfig_should_fail",
			Caller: allowlisttest.TestEnabledAddr,
			InputFn: func(t testing.TB) []byte {
				input, err := acp224feemanager.PackSetFeeConfig(testFeeConfig)
				require.NoError(t, err)
				return input
			},
			SuppliedGas: acp224feemanager.SetFeeConfigGasCost - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas,
		},
		{
			Name:       "set_config_emits_event_with_correct_data",
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := acp224feemanager.PackSetFeeConfig(testFeeConfig)
				require.NoError(t, err)
				return input
			},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().Number().Return(testBlockNumber).AnyTimes()
				mbc.EXPECT().Timestamp().Return(uint64(0)).AnyTimes()
			},
			SuppliedGas: acp224feemanager.SetFeeConfigGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				feeConfig := acp224feemanager.GetStoredFeeConfig(state, acp224feemanager.ContractAddress)
				require.Equal(t, testFeeConfig, feeConfig)

				lastChangedAt := acp224feemanager.GetFeeConfigLastChangedAt(state, acp224feemanager.ContractAddress)
				require.Equal(t, testBlockNumber, lastChangedAt)

				// Verify event was emitted
				logs := state.Logs()
				require.Len(t, logs, 1, "expected one log to be emitted")
				require.Equal(t, acp224feemanager.ContractAddress, logs[0].Address, "expected log address to be precompile address")
			},
		},
	}
)

func TestACP224FeeManagerRun(t *testing.T) {
	precompiletest.RunPrecompileTests(t, acp224feemanager.Module, tests)
}

func TestStoreFeeConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	state := newTestStateDB()

	blockContext := contract.NewMockBlockContext(ctrl)
	blockContext.EXPECT().Number().Return(testBlockNumber).Times(1)

	err := acp224feemanager.StoreFeeConfig(state, acp224feemanager.ContractAddress, testFeeConfig, blockContext)
	require.NoError(t, err)

	got := acp224feemanager.GetStoredFeeConfig(state, acp224feemanager.ContractAddress)
	require.Equal(t, testFeeConfig, got)

	lastChangedAt := acp224feemanager.GetFeeConfigLastChangedAt(state, acp224feemanager.ContractAddress)
	require.Equal(t, testBlockNumber, lastChangedAt)
}

func newTestStateDB() *extstate.StateDB {
	db := rawdb.NewMemoryDatabase()
	statedb, err := state.New(common.Hash{}, state.NewDatabase(db), nil)
	if err != nil {
		panic(err)
	}
	return extstate.New(statedb)
}

func TestPackUnpackSetFeeConfig(t *testing.T) {
	packed, err := acp224feemanager.PackSetFeeConfig(testFeeConfig)
	require.NoError(t, err)

	unpacked, err := acp224feemanager.UnpackSetFeeConfigInput(packed[4:])
	require.NoError(t, err)

	require.Equal(t, testFeeConfig, unpacked)
}

func TestPackUnpackGetFeeConfig(t *testing.T) {
	packed, err := acp224feemanager.PackGetFeeConfigOutput(testFeeConfig)
	require.NoError(t, err)

	unpacked, err := acp224feemanager.UnpackGetFeeConfigOutput(packed)
	require.NoError(t, err)

	require.Equal(t, testFeeConfig, unpacked)
}

// TestPackUnpackFeeConfigUpdatedEventData tests the Pack/UnpackFeeConfigUpdatedEventData.
func TestPackUnpackFeeConfigUpdatedEventData(t *testing.T) {
	oldFeeConfig := commontype.ACP224FeeConfig{
		TargetGas:         big.NewInt(5_000_000),
		MinGasPrice:       common.Big1,
		MaxCapacityFactor: big.NewInt(10),
		TimeToDouble:      big.NewInt(30),
	}
	newFeeConfig := commontype.ACP224FeeConfig{
		TargetGas:         big.NewInt(42_000_000),
		MinGasPrice:       big.NewInt(42),
		MaxCapacityFactor: big.NewInt(42),
		TimeToDouble:      big.NewInt(42),
	}

	_, data, err := acp224feemanager.PackFeeConfigUpdatedEvent(
		allowlisttest.TestEnabledAddr,
		oldFeeConfig,
		newFeeConfig,
	)
	require.NoError(t, err)

	unpacked, err := acp224feemanager.UnpackFeeConfigUpdatedEventData(data)
	require.NoError(t, err)

	expectedData := acp224feemanager.FeeConfigUpdatedEventData{
		OldFeeConfig: oldFeeConfig,
		NewFeeConfig: newFeeConfig,
	}
	require.Equal(t, expectedData, unpacked)
}
