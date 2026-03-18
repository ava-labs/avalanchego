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

	zeroFeeConfig = commontype.ACP224FeeConfig{}

	testBlockNumber = big.NewInt(7)

	tests []precompiletest.PrecompileTest
)

func init() {
	// getFeeConfig tests - all roles should be able to read
	for _, role := range []struct {
		name string
		addr common.Address
	}{
		{"NoRole", allowlisttest.TestNoRoleAddr},
		{"Enabled", allowlisttest.TestEnabledAddr},
		{"Manager", allowlisttest.TestManagerAddr},
		{"Admin", allowlisttest.TestAdminAddr},
	} {
		tests = append(tests, precompiletest.PrecompileTest{
			Name:       "calling_getFeeConfig_from_" + role.name + "_should_succeed",
			Caller:     role.addr,
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
		})
	}

	tests = append(tests,
		precompiletest.PrecompileTest{
			Name:   "get_fee_config_after_setting_returns_new_config",
			Caller: allowlisttest.TestEnabledAddr,
			BeforeHook: func(t testing.TB, state *extstate.StateDB) {
				allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address)(t, state)
				require.NoError(t, acp224feemanager.StoreFeeConfig(state, testFeeConfig, testBlockNumber), "StoreFeeConfig()")
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
		precompiletest.PrecompileTest{
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
	)

	// getFeeConfigLastChangedAt tests - all roles should be able to read
	for _, role := range []struct {
		name string
		addr common.Address
	}{
		{"NoRole", allowlisttest.TestNoRoleAddr},
		{"Enabled", allowlisttest.TestEnabledAddr},
		{"Manager", allowlisttest.TestManagerAddr},
		{"Admin", allowlisttest.TestAdminAddr},
	} {
		tests = append(tests, precompiletest.PrecompileTest{
			Name:   "calling_getFeeConfigLastChangedAt_from_" + role.name + "_should_succeed",
			Caller: role.addr,
			BeforeHook: func(t testing.TB, state *extstate.StateDB) {
				allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address)(t, state)
				require.NoError(t, acp224feemanager.StoreFeeConfig(state, testFeeConfig, testBlockNumber), "StoreFeeConfig()")
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
				feeConfig := acp224feemanager.GetStoredFeeConfig(state)
				require.Equal(t, testFeeConfig, feeConfig, "GetStoredFeeConfig()")
				lastChangedAt := acp224feemanager.GetFeeConfigLastChangedAt(state)
				require.Equal(t, testBlockNumber, lastChangedAt, "GetFeeConfigLastChangedAt()")
			},
		})
	}

	tests = append(tests, precompiletest.PrecompileTest{
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
	})

	// setFeeConfig tests - NoRole should fail
	tests = append(tests, precompiletest.PrecompileTest{
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
	})

	// setFeeConfig tests - Enabled, Manager, and Admin should succeed
	for _, role := range []struct {
		name string
		addr common.Address
	}{
		{"Enabled", allowlisttest.TestEnabledAddr},
		{"Manager", allowlisttest.TestManagerAddr},
		{"Admin", allowlisttest.TestAdminAddr},
	} {
		tests = append(tests, precompiletest.PrecompileTest{
			Name:       "calling_setFeeConfig_from_" + role.name + "_should_succeed",
			Caller:     role.addr,
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
				feeConfig := acp224feemanager.GetStoredFeeConfig(state)
				require.Equal(t, testFeeConfig, feeConfig, "GetStoredFeeConfig()")
			},
		})
	}

	tests = append(tests,
		precompiletest.PrecompileTest{
			Name:       "readOnly_setFeeConfig_should_fail",
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := acp224feemanager.PackSetFeeConfig(testFeeConfig)
				require.NoError(t, err)
				return input
			},
			SuppliedGas: acp224feemanager.SetFeeConfigGasCost,
			ReadOnly:    true,
			ExpectedErr: vm.ErrWriteProtection,
		},
		precompiletest.PrecompileTest{
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
		precompiletest.PrecompileTest{
			Name:       "setFeeConfig_with_invalid_config_should_fail",
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(acp224feemanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				invalidConfig := commontype.ACP224FeeConfig{
					TargetGas:    commontype.MinTargetGasACP224,
					MinGasPrice:  0, // invalid: must be > 0
					TimeToDouble: 60,
				}
				input, err := acp224feemanager.PackSetFeeConfig(invalidConfig)
				require.NoError(t, err)
				return input
			},
			SuppliedGas: acp224feemanager.SetFeeConfigGasCost,
			ReadOnly:    false,
			ExpectedErr: commontype.ErrMinGasPriceTooLow,
		},
		precompiletest.PrecompileTest{
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
				feeConfig := acp224feemanager.GetStoredFeeConfig(state)
				require.Equal(t, testFeeConfig, feeConfig, "GetStoredFeeConfig()")

				lastChangedAt := acp224feemanager.GetFeeConfigLastChangedAt(state)
				require.Equal(t, testBlockNumber, lastChangedAt, "GetFeeConfigLastChangedAt()")

				// Verify event was emitted with correct topics and data
				logs := state.Logs()
				require.Len(t, logs, 1, "expected one log to be emitted")
				log := logs[0]
				require.Equal(t, acp224feemanager.ContractAddress, log.Address, "log address")

				// Topic[0] is the event signature hash, Topic[1] is the indexed sender
				require.Len(t, log.Topics, 2, "expected 2 topics (event sig + indexed sender)")
				wantTopics, _, err := acp224feemanager.PackFeeConfigUpdatedEvent(
					allowlisttest.TestEnabledAddr,
					zeroFeeConfig, // old config is zero since no prior config was stored
					testFeeConfig,
				)
				require.NoError(t, err, "PackFeeConfigUpdatedEvent()")
				require.Equal(t, wantTopics, log.Topics, "event topics")

				// Verify non-indexed event data round-trips correctly
				unpacked, err := acp224feemanager.UnpackFeeConfigUpdatedEventData(log.Data)
				require.NoError(t, err, "UnpackFeeConfigUpdatedEventData()")
				require.Equal(t, zeroFeeConfig, unpacked.OldFeeConfig, "old fee config in event")
				require.Equal(t, testFeeConfig, unpacked.NewFeeConfig, "new fee config in event")
			},
		},
	)
}

func TestACP224FeeManagerRun(t *testing.T) {
	precompiletest.RunPrecompileTests(t, acp224feemanager.Module, tests)
}

func TestStoreFeeConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      commontype.ACP224FeeConfig
		blockNumber *big.Int
		wantErr     error
	}{
		{
			name:        "valid config",
			config:      testFeeConfig,
			blockNumber: testBlockNumber,
		},
		{
			name:        "nil block number",
			config:      testFeeConfig,
			blockNumber: nil,
			wantErr:     acp224feemanager.ErrNilBlockNumber,
		},
		{
			name: "invalid config",
			config: commontype.ACP224FeeConfig{
				TargetGas:    commontype.MinTargetGasACP224,
				MinGasPrice:  0, // invalid: must be > 0
				TimeToDouble: 60,
			},
			blockNumber: testBlockNumber,
			wantErr:     commontype.ErrMinGasPriceTooLow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStateDB()
			err := acp224feemanager.StoreFeeConfig(s, tt.config, tt.blockNumber)
			require.ErrorIs(t, err, tt.wantErr, "StoreFeeConfig()")
			if tt.wantErr != nil {
				return
			}

			got := acp224feemanager.GetStoredFeeConfig(s)
			require.Equal(t, tt.config, got, "GetStoredFeeConfig()")

			lastChangedAt := acp224feemanager.GetFeeConfigLastChangedAt(s)
			require.Equal(t, tt.blockNumber, lastChangedAt, "GetFeeConfigLastChangedAt()")
		})
	}
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
	require.NoError(t, err, "PackSetFeeConfig()")

	unpacked, err := acp224feemanager.UnpackSetFeeConfigInput(packed[4:])
	require.NoError(t, err, "UnpackSetFeeConfigInput()")

	require.Equal(t, testFeeConfig, unpacked, "round-trip pack/unpack setFeeConfig")
}

func TestPackUnpackGetFeeConfig(t *testing.T) {
	packed, err := acp224feemanager.PackGetFeeConfigOutput(testFeeConfig)
	require.NoError(t, err, "PackGetFeeConfigOutput()")

	unpacked, err := acp224feemanager.UnpackGetFeeConfigOutput(packed)
	require.NoError(t, err, "UnpackGetFeeConfigOutput()")

	require.Equal(t, testFeeConfig, unpacked, "round-trip pack/unpack getFeeConfig")
}

func TestPackUnpackFeeConfigUpdatedEventData(t *testing.T) {
	oldFeeConfig := commontype.ACP224FeeConfig{
		TargetGas:    5_000_000,
		MinGasPrice:  1,
		TimeToDouble: 30,
	}
	newFeeConfig := commontype.ACP224FeeConfig{
		TargetGas:    42_000_000,
		MinGasPrice:  42,
		TimeToDouble: 42,
	}

	_, data, err := acp224feemanager.PackFeeConfigUpdatedEvent(
		allowlisttest.TestEnabledAddr,
		oldFeeConfig,
		newFeeConfig,
	)
	require.NoError(t, err, "PackFeeConfigUpdatedEvent()")

	unpacked, err := acp224feemanager.UnpackFeeConfigUpdatedEventData(data)
	require.NoError(t, err, "UnpackFeeConfigUpdatedEventData()")

	want := acp224feemanager.FeeConfigUpdatedEventData{
		OldFeeConfig: oldFeeConfig,
		NewFeeConfig: newFeeConfig,
	}
	require.Equal(t, want, unpacked, "round-trip pack/unpack FeeConfigUpdatedEvent")
}
