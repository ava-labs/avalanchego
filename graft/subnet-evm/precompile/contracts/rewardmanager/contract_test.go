// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rewardmanager_test

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/graft/evm/constants"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core/extstate"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist/allowlisttest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/rewardmanager"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompiletest"

	ethtypes "github.com/ava-labs/libevm/core/types"
)

var (
	rewardAddress = common.HexToAddress("0x0123")
	tests         = []precompiletest.PrecompileTest{
		{
			Name:       "set_allow_fee_recipients_from_no_role_fails",
			Caller:     allowlisttest.TestNoRoleAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(rewardmanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := rewardmanager.PackAllowFeeRecipients()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: rewardmanager.AllowFeeRecipientsGasCost,
			ReadOnly:    false,
			ExpectedErr: rewardmanager.ErrCannotAllowFeeRecipients,
		},
		{
			Name:       "set_reward_address_from_no_role_fails",
			Caller:     allowlisttest.TestNoRoleAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(rewardmanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := rewardmanager.PackSetRewardAddress(rewardAddress)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: rewardmanager.SetRewardAddressGasCost,
			ReadOnly:    false,
			ExpectedErr: rewardmanager.ErrCannotSetRewardAddress,
		},
		{
			Name:       "disable_rewards_from_no_role_fails",
			Caller:     allowlisttest.TestNoRoleAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(rewardmanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := rewardmanager.PackDisableRewards()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: rewardmanager.DisableRewardsGasCost,
			ReadOnly:    false,
			ExpectedErr: rewardmanager.ErrCannotDisableRewards,
		},
		{
			Name:       "set_allow_fee_recipients_from_enabled_succeeds",
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(rewardmanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := rewardmanager.PackAllowFeeRecipients()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: rewardmanager.AllowFeeRecipientsGasCost + rewardmanager.FeeRecipientsAllowedEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				_, isFeeRecipients := rewardmanager.GetStoredRewardAddress(state)
				require.True(t, isFeeRecipients)

				logs := state.Logs()
				assertFeeRecipientsAllowed(t, logs, allowlisttest.TestEnabledAddr)
			},
		},
		{
			Name:       "set_fee_recipients_should_not_emit_events_pre_Durango",
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(rewardmanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := rewardmanager.PackAllowFeeRecipients()
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
			SuppliedGas: rewardmanager.AllowFeeRecipientsGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, stateDB *extstate.StateDB) {
				// Check no logs are stored in state
				logs := stateDB.Logs()
				require.Empty(t, logs)
			},
		},
		{
			Name:       "set_reward_address_from_enabled_succeeds",
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(rewardmanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := rewardmanager.PackSetRewardAddress(rewardAddress)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: rewardmanager.SetRewardAddressGasCost + rewardmanager.RewardAddressChangedEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				address, isFeeRecipients := rewardmanager.GetStoredRewardAddress(state)
				require.Equal(t, rewardAddress, address)
				require.False(t, isFeeRecipients)

				logs := state.Logs()
				assertRewardAddressChanged(t, logs, allowlisttest.TestEnabledAddr, common.Address{}, rewardAddress)
			},
		},
		{
			Name:       "set_allow_fee_recipients_from_manager_succeeds",
			Caller:     allowlisttest.TestManagerAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(rewardmanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := rewardmanager.PackAllowFeeRecipients()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: rewardmanager.AllowFeeRecipientsGasCost + rewardmanager.FeeRecipientsAllowedEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				_, isFeeRecipients := rewardmanager.GetStoredRewardAddress(state)
				require.True(t, isFeeRecipients)

				logs := state.Logs()
				assertFeeRecipientsAllowed(t, logs, allowlisttest.TestManagerAddr)
			},
		},
		{
			Name:       "set_reward_address_from_manager_succeeds",
			Caller:     allowlisttest.TestManagerAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(rewardmanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := rewardmanager.PackSetRewardAddress(rewardAddress)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: rewardmanager.SetRewardAddressGasCost + rewardmanager.RewardAddressChangedEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				address, isFeeRecipients := rewardmanager.GetStoredRewardAddress(state)
				require.Equal(t, rewardAddress, address)
				require.False(t, isFeeRecipients)

				logs := state.Logs()
				assertRewardAddressChanged(t, logs, allowlisttest.TestManagerAddr, common.Address{}, rewardAddress)
			},
		},
		{
			Name:       "change_reward_address_should_not_emit_events_pre_Durango",
			Caller:     allowlisttest.TestManagerAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(rewardmanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := rewardmanager.PackSetRewardAddress(rewardAddress)
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
			SuppliedGas: rewardmanager.SetRewardAddressGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, stateDB *extstate.StateDB) {
				// Check no logs are stored in state
				logs := stateDB.Logs()
				require.Empty(t, logs)
			},
		},
		{
			Name:       "disable_rewards_from_manager_succeeds",
			Caller:     allowlisttest.TestManagerAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(rewardmanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := rewardmanager.PackDisableRewards()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: rewardmanager.DisableRewardsGasCost + rewardmanager.RewardsDisabledEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				address, isFeeRecipients := rewardmanager.GetStoredRewardAddress(state)
				require.False(t, isFeeRecipients)
				require.Equal(t, constants.BlackholeAddr, address)

				logs := state.Logs()
				assertRewardsDisabled(t, logs, allowlisttest.TestManagerAddr)
			},
		},
		{
			Name:       "disable_rewards_from_enabled_succeeds",
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(rewardmanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := rewardmanager.PackDisableRewards()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: rewardmanager.DisableRewardsGasCost + rewardmanager.RewardsDisabledEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				address, isFeeRecipients := rewardmanager.GetStoredRewardAddress(state)
				require.False(t, isFeeRecipients)
				require.Equal(t, constants.BlackholeAddr, address)

				logs := state.Logs()
				assertRewardsDisabled(t, logs, allowlisttest.TestEnabledAddr)
			},
		},
		{
			Name:       "disable_rewards_should_not_emit_event_pre_Durango",
			Caller:     allowlisttest.TestManagerAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(rewardmanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := rewardmanager.PackDisableRewards()
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
			SuppliedGas: rewardmanager.SetRewardAddressGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, stateDB *extstate.StateDB) {
				// Check logs are not stored in state
				logs := stateDB.Logs()
				require.Empty(t, logs)
			},
		},
		{
			Name:   "get_current_reward_address_from_no_role_succeeds",
			Caller: allowlisttest.TestNoRoleAddr,
			BeforeHook: func(t testing.TB, state *extstate.StateDB) {
				allowlisttest.SetDefaultRoles(rewardmanager.Module.Address)(t, state)
				rewardmanager.StoreRewardAddress(state, rewardAddress)
			},
			InputFn: func(t testing.TB) []byte {
				input, err := rewardmanager.PackCurrentRewardAddress()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: rewardmanager.CurrentRewardAddressGasCost,
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := rewardmanager.PackCurrentRewardAddressOutput(rewardAddress)
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		{
			Name:   "get_are_fee_recipients_allowed_from_no_role_succeeds",
			Caller: allowlisttest.TestNoRoleAddr,
			BeforeHook: func(t testing.TB, state *extstate.StateDB) {
				allowlisttest.SetDefaultRoles(rewardmanager.Module.Address)(t, state)
				rewardmanager.EnableAllowFeeRecipients(state)
			},
			InputFn: func(t testing.TB) []byte {
				input, err := rewardmanager.PackAreFeeRecipientsAllowed()
				require.NoError(t, err)
				return input
			},
			SuppliedGas: rewardmanager.AreFeeRecipientsAllowedGasCost,
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := rewardmanager.PackAreFeeRecipientsAllowedOutput(true)
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		{
			Name:       "get_initial_config_with_address",
			Caller:     allowlisttest.TestNoRoleAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(rewardmanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := rewardmanager.PackCurrentRewardAddress()
				require.NoError(t, err)
				return input
			},
			SuppliedGas: rewardmanager.CurrentRewardAddressGasCost,
			Config: &rewardmanager.Config{
				InitialRewardConfig: &rewardmanager.InitialRewardConfig{
					RewardAddress: rewardAddress,
				},
			},
			ReadOnly: false,
			ExpectedRes: func() []byte {
				res, err := rewardmanager.PackCurrentRewardAddressOutput(rewardAddress)
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		{
			Name:       "get_initial_config_with_allow_fee_recipients_enabled",
			Caller:     allowlisttest.TestNoRoleAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(rewardmanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := rewardmanager.PackAreFeeRecipientsAllowed()
				require.NoError(t, err)
				return input
			},
			SuppliedGas: rewardmanager.AreFeeRecipientsAllowedGasCost,
			Config: &rewardmanager.Config{
				InitialRewardConfig: &rewardmanager.InitialRewardConfig{
					AllowFeeRecipients: true,
				},
			},
			ReadOnly: false,
			ExpectedRes: func() []byte {
				res, err := rewardmanager.PackAreFeeRecipientsAllowedOutput(true)
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		{
			Name:       "readOnly_allow_fee_recipients_with_allowed_role_fails",
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(rewardmanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := rewardmanager.PackAllowFeeRecipients()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: rewardmanager.AllowFeeRecipientsGasCost,
			ReadOnly:    true,
			ExpectedErr: vm.ErrWriteProtection,
		},
		{
			Name:       "readOnly_set_reward_address_with_allowed_role_fails",
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(rewardmanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := rewardmanager.PackSetRewardAddress(rewardAddress)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: rewardmanager.SetRewardAddressGasCost,
			ReadOnly:    true,
			ExpectedErr: vm.ErrWriteProtection,
		},
		{
			Name:       "insufficient_gas_set_reward_address_from_allowed_role",
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(rewardmanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := rewardmanager.PackSetRewardAddress(rewardAddress)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: rewardmanager.SetRewardAddressGasCost + rewardmanager.RewardAddressChangedEventGasCost - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas,
		},
		{
			Name:       "insufficient_gas_allow_fee_recipients_from_allowed_role",
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(rewardmanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := rewardmanager.PackAllowFeeRecipients()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: rewardmanager.AllowFeeRecipientsGasCost + rewardmanager.FeeRecipientsAllowedEventGasCost - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas,
		},
		{
			Name:       "insufficient_gas_read_current_reward_address_from_allowed_role",
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(rewardmanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := rewardmanager.PackCurrentRewardAddress()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: rewardmanager.CurrentRewardAddressGasCost - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas,
		},
		{
			Name:       "insufficient_gas_are_fee_recipients_allowed_from_allowed_role",
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(rewardmanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := rewardmanager.PackAreFeeRecipientsAllowed()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: rewardmanager.AreFeeRecipientsAllowedGasCost - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas,
		},
		{
			Name:       "set_reward_address_invalid_length_pre_durango",
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(rewardmanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				// Create invalid input with extra bytes
				input, err := rewardmanager.PackSetRewardAddress(rewardAddress)
				require.NoError(t, err)
				return append(input, []byte{0, 0}...)
			},
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				mockChainConfig := precompileconfig.NewMockChainConfig(ctrl)
				mockChainConfig.EXPECT().GetFeeConfig().AnyTimes().Return(commontype.ValidTestFeeConfig)
				mockChainConfig.EXPECT().AllowedFeeRecipients().AnyTimes().Return(false)
				mockChainConfig.EXPECT().IsDurango(gomock.Any()).AnyTimes().Return(false)
				return mockChainConfig
			},
			SuppliedGas: rewardmanager.SetRewardAddressGasCost,
			ReadOnly:    false,
			ExpectedErr: rewardmanager.ErrInvalidLen,
		},
		{
			Name:       "set_reward_address_invalid_length_post_durango",
			Caller:     allowlisttest.TestEnabledAddr,
			BeforeHook: allowlisttest.SetDefaultRoles(rewardmanager.Module.Address),
			InputFn: func(t testing.TB) []byte {
				// Create invalid input with extra bytes
				input, err := rewardmanager.PackSetRewardAddress(rewardAddress)
				require.NoError(t, err)
				return append(input, []byte{0, 0}...)
			},
			SuppliedGas: rewardmanager.SetRewardAddressGasCost,
			ReadOnly:    false,
		},
	}
)

func TestRewardManagerRun(t *testing.T) {
	allowlisttest.RunPrecompileWithAllowListTests(t, rewardmanager.Module, tests)
}

func assertRewardAddressChanged(
	t testing.TB,
	logs []*ethtypes.Log,
	caller,
	oldAddress,
	newAddress common.Address,
) {
	require.Len(t, logs, 1)
	log := logs[0]
	require.Equal(
		t,
		[]common.Hash{
			rewardmanager.RewardManagerABI.Events["RewardAddressChanged"].ID,
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
	caller common.Address,
) {
	require.Len(t, logs, 1)
	log := logs[0]
	require.Equal(
		t,
		[]common.Hash{
			rewardmanager.RewardManagerABI.Events["RewardsDisabled"].ID,
			common.BytesToHash(caller[:]),
		},
		log.Topics,
	)
	require.Empty(t, log.Data)
}

func assertFeeRecipientsAllowed(
	t testing.TB,
	logs []*ethtypes.Log,
	caller common.Address,
) {
	require.Len(t, logs, 1)
	log := logs[0]
	require.Equal(
		t,
		[]common.Hash{
			rewardmanager.RewardManagerABI.Events["FeeRecipientsAllowed"].ID,
			common.BytesToHash(caller[:]),
		},
		log.Topics,
	)
	require.Empty(t, log.Data)
}
