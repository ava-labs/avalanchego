// (c) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rewardmanager

import (
	"testing"

	"github.com/ava-labs/subnet-evm/constants"
	"github.com/ava-labs/subnet-evm/core/state"
	"github.com/ava-labs/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/subnet-evm/precompile/contract"
	"github.com/ava-labs/subnet-evm/precompile/testutils"
	"github.com/ava-labs/subnet-evm/vmerrs"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

var (
	testAddr = common.HexToAddress("0x0123")
	tests    = map[string]testutils.PrecompileTest{
		"set allow fee recipients from no role fails": {
			Caller:     allowlist.TestNoRoleAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
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
			Caller:     allowlist.TestNoRoleAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetRewardAddress(testAddr)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: SetRewardAddressGasCost,
			ReadOnly:    false,
			ExpectedErr: ErrCannotSetRewardAddress.Error(),
		},
		"disable rewards from no role fails": {
			Caller:     allowlist.TestNoRoleAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
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
			Caller:     allowlist.TestEnabledAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackAllowFeeRecipients()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: AllowFeeRecipientsGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state contract.StateDB) {
				_, isFeeRecipients := GetStoredRewardAddress(state)
				require.True(t, isFeeRecipients)
			},
		},
		"set reward address from enabled succeeds": {
			Caller:     allowlist.TestEnabledAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetRewardAddress(testAddr)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: SetRewardAddressGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state contract.StateDB) {
				address, isFeeRecipients := GetStoredRewardAddress(state)
				require.Equal(t, testAddr, address)
				require.False(t, isFeeRecipients)
			},
		},
		"disable rewards from enabled succeeds": {
			Caller:     allowlist.TestEnabledAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackDisableRewards()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: DisableRewardsGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state contract.StateDB) {
				address, isFeeRecipients := GetStoredRewardAddress(state)
				require.False(t, isFeeRecipients)
				require.Equal(t, constants.BlackholeAddr, address)
			},
		},
		"get current reward address from no role succeeds": {
			Caller: allowlist.TestNoRoleAddr,
			BeforeHook: func(t testing.TB, state contract.StateDB) {
				allowlist.SetDefaultRoles(Module.Address)(t, state)
				StoreRewardAddress(state, testAddr)
			},
			InputFn: func(t testing.TB) []byte {
				input, err := PackCurrentRewardAddress()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: CurrentRewardAddressGasCost,
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackCurrentRewardAddressOutput(testAddr)
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		"get are fee recipients allowed from no role succeeds": {
			Caller: allowlist.TestNoRoleAddr,
			BeforeHook: func(t testing.TB, state contract.StateDB) {
				allowlist.SetDefaultRoles(Module.Address)(t, state)
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
			Caller:     allowlist.TestNoRoleAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackCurrentRewardAddress()
				require.NoError(t, err)
				return input
			},
			SuppliedGas: CurrentRewardAddressGasCost,
			Config: &Config{
				InitialRewardConfig: &InitialRewardConfig{
					RewardAddress: testAddr,
				},
			},
			ReadOnly: false,
			ExpectedRes: func() []byte {
				res, err := PackCurrentRewardAddressOutput(testAddr)
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		"get initial config with allow fee recipients enabled": {
			Caller:     allowlist.TestNoRoleAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
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
			Caller:     allowlist.TestEnabledAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackAllowFeeRecipients()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: AllowFeeRecipientsGasCost,
			ReadOnly:    true,
			ExpectedErr: vmerrs.ErrWriteProtection.Error(),
		},
		"readOnly set reward addresss with allowed role fails": {
			Caller:     allowlist.TestEnabledAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetRewardAddress(testAddr)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: SetRewardAddressGasCost,
			ReadOnly:    true,
			ExpectedErr: vmerrs.ErrWriteProtection.Error(),
		},
		"insufficient gas set reward address from allowed role": {
			Caller:     allowlist.TestEnabledAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackSetRewardAddress(testAddr)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: SetRewardAddressGasCost - 1,
			ReadOnly:    false,
			ExpectedErr: vmerrs.ErrOutOfGas.Error(),
		},
		"insufficient gas allow fee recipients from allowed role": {
			Caller:     allowlist.TestEnabledAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackAllowFeeRecipients()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: AllowFeeRecipientsGasCost - 1,
			ReadOnly:    false,
			ExpectedErr: vmerrs.ErrOutOfGas.Error(),
		},
		"insufficient gas read current reward address from allowed role": {
			Caller:     allowlist.TestEnabledAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackCurrentRewardAddress()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: CurrentRewardAddressGasCost - 1,
			ReadOnly:    false,
			ExpectedErr: vmerrs.ErrOutOfGas.Error(),
		},
		"insufficient gas are fee recipients allowed from allowed role": {
			Caller:     allowlist.TestEnabledAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackAreFeeRecipientsAllowed()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: AreFeeRecipientsAllowedGasCost - 1,
			ReadOnly:    false,
			ExpectedErr: vmerrs.ErrOutOfGas.Error(),
		},
	}
)

func TestRewardManagerRun(t *testing.T) {
	allowlist.RunPrecompileWithAllowListTests(t, Module, state.NewTestStateDB, tests)
}

func BenchmarkRewardManager(b *testing.B) {
	allowlist.BenchPrecompileWithAllowList(b, Module, state.NewTestStateDB, tests)
}
