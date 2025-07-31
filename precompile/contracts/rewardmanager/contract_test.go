// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rewardmanager

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	ethtypes "github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/constants"
	"github.com/ava-labs/subnet-evm/core/extstate"
	"github.com/ava-labs/subnet-evm/precompile/allowlist/allowlisttest"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/subnet-evm/precompile/precompiletest"
)

var (
	rewardAddress = common.HexToAddress("0x0123")
	tests         = map[string]precompiletest.PrecompileTest{
		"set allow fee recipients from no role fails": {
			Caller:     allowlisttest.TestNoRoleAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackAllowFeeRecipients()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: AllowFeeRecipientsGasCost,
			ReadOnly:    false,
			ExpectedErr: ErrCannotAllowFeeRecipients.Error(),
		},
		"set reward address from no role fails": {
			Caller:     allowlisttest.TestNoRoleAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetRewardAddress(rewardAddress)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: SetRewardAddressGasCost,
			ReadOnly:    false,
			ExpectedErr: ErrCannotSetRewardAddress.Error(),
		},
		"disable rewards from no role fails": {
			Caller:     allowlisttest.TestNoRoleAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackDisableRewards()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: DisableRewardsGasCost,
			ReadOnly:    false,
			ExpectedErr: ErrCannotDisableRewards.Error(),
		},
		"set allow fee recipients from enabled succeeds": {
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackAllowFeeRecipients()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: AllowFeeRecipientsGasCost + FeeRecipientsAllowedEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				_, isFeeRecipients := GetStoredRewardAddress(state)
				require.True(t, isFeeRecipients)

				logs := state.Logs()
				assertFeeRecipientsAllowed(t, logs, allowlisttest.TestEnabledAddr)
			},
		},
		"set fee recipients should not emit events pre-Durango": {
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackAllowFeeRecipients()
				require.NoError(t, err)

				return input
			},
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				mockChainConfig := precompileconfig.NewMockChainConfig(ctrl)
				mockChainConfig.EXPECT().GetFeeConfig().AnyTimes().Return(commontype.ValidTestFeeConfig)
				mockChainConfig.EXPECT().AllowedFeeRecipients().AnyTimes().Return(false)
				mockChainConfig.EXPECT().IsDurango(gomock.Any()).AnyTimes().Return(false)
				return mockChainConfig
			},
			SuppliedGas: AllowFeeRecipientsGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, stateDB *extstate.StateDB) {
				// Check no logs are stored in state
				logs := stateDB.Logs()
				require.Empty(t, logs)
			},
		},
		"set reward address from enabled succeeds": {
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetRewardAddress(rewardAddress)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: SetRewardAddressGasCost + RewardAddressChangedEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				address, isFeeRecipients := GetStoredRewardAddress(state)
				require.Equal(t, rewardAddress, address)
				require.False(t, isFeeRecipients)

				logs := state.Logs()
				assertRewardAddressChanged(t, logs, allowlisttest.TestEnabledAddr, common.Address{}, rewardAddress)
			},
		},
		"set allow fee recipients from manager succeeds": {
			Caller:     allowlisttest.TestManagerAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackAllowFeeRecipients()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: AllowFeeRecipientsGasCost + FeeRecipientsAllowedEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				_, isFeeRecipients := GetStoredRewardAddress(state)
				require.True(t, isFeeRecipients)

				logs := state.Logs()
				assertFeeRecipientsAllowed(t, logs, allowlisttest.TestManagerAddr)
			},
		},
		"set reward address from manager succeeds": {
			Caller:     allowlisttest.TestManagerAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetRewardAddress(rewardAddress)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: SetRewardAddressGasCost + RewardAddressChangedEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				address, isFeeRecipients := GetStoredRewardAddress(state)
				require.Equal(t, rewardAddress, address)
				require.False(t, isFeeRecipients)

				logs := state.Logs()
				assertRewardAddressChanged(t, logs, allowlisttest.TestManagerAddr, common.Address{}, rewardAddress)
			},
		},
		"change reward address should not emit events pre-Durango": {
			Caller:     allowlisttest.TestManagerAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetRewardAddress(rewardAddress)
				require.NoError(t, err)

				return input
			},
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				mockChainConfig := precompileconfig.NewMockChainConfig(ctrl)
				mockChainConfig.EXPECT().GetFeeConfig().AnyTimes().Return(commontype.ValidTestFeeConfig)
				mockChainConfig.EXPECT().AllowedFeeRecipients().AnyTimes().Return(false)
				mockChainConfig.EXPECT().IsDurango(gomock.Any()).AnyTimes().Return(false)
				return mockChainConfig
			},
			SuppliedGas: SetRewardAddressGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, stateDB *extstate.StateDB) {
				// Check no logs are stored in state
				logs := stateDB.Logs()
				require.Empty(t, logs)
			},
		},
		"disable rewards from manager succeeds": {
			Caller:     allowlisttest.TestManagerAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackDisableRewards()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: DisableRewardsGasCost + RewardsDisabledEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				address, isFeeRecipients := GetStoredRewardAddress(state)
				require.False(t, isFeeRecipients)
				require.Equal(t, constants.BlackholeAddr, address)

				logs := state.Logs()
				assertRewardsDisabled(t, logs, allowlisttest.TestManagerAddr)
			},
		},
		"disable rewards from enabled succeeds": {
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackDisableRewards()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: DisableRewardsGasCost + RewardsDisabledEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				address, isFeeRecipients := GetStoredRewardAddress(state)
				require.False(t, isFeeRecipients)
				require.Equal(t, constants.BlackholeAddr, address)

				logs := state.Logs()
				assertRewardsDisabled(t, logs, allowlisttest.TestEnabledAddr)
			},
		},
		"disable rewards should not emit event pre-Durango": {
			Caller:     allowlisttest.TestManagerAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackDisableRewards()
				require.NoError(t, err)

				return input
			},
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				mockChainConfig := precompileconfig.NewMockChainConfig(ctrl)
				mockChainConfig.EXPECT().GetFeeConfig().AnyTimes().Return(commontype.ValidTestFeeConfig)
				mockChainConfig.EXPECT().AllowedFeeRecipients().AnyTimes().Return(false)
				mockChainConfig.EXPECT().IsDurango(gomock.Any()).AnyTimes().Return(false)
				return mockChainConfig
			},
			SuppliedGas: SetRewardAddressGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, stateDB *extstate.StateDB) {
				// Check logs are not stored in state
				logs := stateDB.Logs()
				require.Empty(t, logs)
			},
		},
		"get current reward address from no role succeeds": {
			Caller: allowlisttest.TestNoRoleAddr,
			BeforeHook: func(t testing.TB, state *extstate.StateDB) {
				allowlisttest.SetDefaultRoles(Module.Address)(t, state)
				StoreRewardAddress(state, rewardAddress)
			},
			InputFn: func(t testing.TB) []byte {
				input, err := PackCurrentRewardAddress()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: CurrentRewardAddressGasCost,
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackCurrentRewardAddressOutput(rewardAddress)
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		"get are fee recipients allowed from no role succeeds": {
			Caller: allowlisttest.TestNoRoleAddr,
			BeforeHook: func(t testing.TB, state *extstate.StateDB) {
				allowlisttest.SetDefaultRoles(Module.Address)(t, state)
				EnableAllowFeeRecipients(state)
			},
			InputFn: func(t testing.TB) []byte {
				input, err := PackAreFeeRecipientsAllowed()
				require.NoError(t, err)
				return input
			},
			SuppliedGas: AreFeeRecipientsAllowedGasCost,
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackAreFeeRecipientsAllowedOutput(true)
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		"get initial config with address": {
			Caller:     allowlisttest.TestNoRoleAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackCurrentRewardAddress()
				require.NoError(t, err)
				return input
			},
			SuppliedGas: CurrentRewardAddressGasCost,
			Config: &Config{
				InitialRewardConfig: &InitialRewardConfig{
					RewardAddress: rewardAddress,
				},
			},
			ReadOnly: false,
			ExpectedRes: func() []byte {
				res, err := PackCurrentRewardAddressOutput(rewardAddress)
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		"get initial config with allow fee recipients enabled": {
			Caller:     allowlisttest.TestNoRoleAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackAreFeeRecipientsAllowed()
				require.NoError(t, err)
				return input
			},
			SuppliedGas: AreFeeRecipientsAllowedGasCost,
			Config: &Config{
				InitialRewardConfig: &InitialRewardConfig{
					AllowFeeRecipients: true,
				},
			},
			ReadOnly: false,
			ExpectedRes: func() []byte {
				res, err := PackAreFeeRecipientsAllowedOutput(true)
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		"readOnly allow fee recipients with allowed role fails": {
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackAllowFeeRecipients()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: AllowFeeRecipientsGasCost,
			ReadOnly:    true,
			ExpectedErr: vm.ErrWriteProtection.Error(),
		},
		"readOnly set reward address with allowed role fails": {
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetRewardAddress(rewardAddress)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: SetRewardAddressGasCost,
			ReadOnly:    true,
			ExpectedErr: vm.ErrWriteProtection.Error(),
		},
		"insufficient gas set reward address from allowed role": {
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetRewardAddress(rewardAddress)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: SetRewardAddressGasCost + RewardAddressChangedEventGasCost - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
		"insufficient gas allow fee recipients from allowed role": {
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackAllowFeeRecipients()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: AllowFeeRecipientsGasCost + FeeRecipientsAllowedEventGasCost - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
		"insufficient gas read current reward address from allowed role": {
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackCurrentRewardAddress()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: CurrentRewardAddressGasCost - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
		"insufficient gas are fee recipients allowed from allowed role": {
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackAreFeeRecipientsAllowed()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: AreFeeRecipientsAllowedGasCost - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
	}
)

func TestRewardManagerRun(t *testing.T) {
	allowlisttest.RunPrecompileWithAllowListTests(t, Module, tests)
}

func assertRewardAddressChanged(
	t testing.TB,
	logs []*ethtypes.Log,
	caller,
	oldAddress,
	newAddress common.Address) {
	require.Len(t, logs, 1)
	log := logs[0]
	require.Equal(
		t,
		[]common.Hash{
			RewardManagerABI.Events["RewardAddressChanged"].ID,
			common.BytesToHash(caller[:]),
			common.BytesToHash(oldAddress[:]),
			common.BytesToHash(newAddress[:]),
		},
		log.Topics,
	)
	require.Empty(t, log.Data)
}

func assertRewardsDisabled(
	t testing.TB,
	logs []*ethtypes.Log,
	caller common.Address) {
	require.Len(t, logs, 1)
	log := logs[0]
	require.Equal(
		t,
		[]common.Hash{
			RewardManagerABI.Events["RewardsDisabled"].ID,
			common.BytesToHash(caller[:]),
		},
		log.Topics,
	)
	require.Empty(t, log.Data)
}

func assertFeeRecipientsAllowed(
	t testing.TB,
	logs []*ethtypes.Log,
	caller common.Address) {
	require.Len(t, logs, 1)
	log := logs[0]
	require.Equal(
		t,
		[]common.Hash{
			RewardManagerABI.Events["FeeRecipientsAllowed"].ID,
			common.BytesToHash(caller[:]),
		},
		log.Topics,
	)
	require.Empty(t, log.Data)
}
