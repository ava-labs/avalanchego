// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nativeminter_test

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/math"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/core/extstate"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist/allowlisttest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/nativeminter"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompiletest"

	ethtypes "github.com/ava-labs/libevm/core/types"
)

var tests = []precompiletest.PrecompileTest{
	{
		Name:       "calling_mintNativeCoin_from_NoRole_should_fail",
		Caller:     allowlisttest.TestNoRoleAddr,
		BeforeHook: allowlisttest.SetDefaultRoles(nativeminter.Module.Address),
		InputFn: func(t testing.TB) []byte {
			input, err := nativeminter.PackMintNativeCoin(allowlisttest.TestNoRoleAddr, common.Big1)
			require.NoError(t, err)

			return input
		},
		SuppliedGas: nativeminter.MintGasCost,
		ReadOnly:    false,
		ExpectedErr: nativeminter.ErrCannotMint,
	},
	{
		Name:       "calling_mintNativeCoin_from_Enabled_should_succeed",
		Caller:     allowlisttest.TestEnabledAddr,
		BeforeHook: allowlisttest.SetDefaultRoles(nativeminter.Module.Address),
		InputFn: func(t testing.TB) []byte {
			input, err := nativeminter.PackMintNativeCoin(allowlisttest.TestEnabledAddr, common.Big1)
			require.NoError(t, err)

			return input
		},
		SuppliedGas: nativeminter.MintGasCost + nativeminter.NativeCoinMintedEventGasCost,
		ReadOnly:    false,
		ExpectedRes: []byte{},
		AfterHook: func(t testing.TB, stateDB *extstate.StateDB) {
			expected := uint256.MustFromBig(common.Big1)
			require.Equal(t, expected, stateDB.GetBalance(allowlisttest.TestEnabledAddr), "expected minted funds")

			logs := stateDB.Logs()
			assertNativeCoinMintedEvent(t, logs, allowlisttest.TestEnabledAddr, allowlisttest.TestEnabledAddr, common.Big1)
		},
	},
	{
		Name:       "initial_mint_funds",
		Caller:     allowlisttest.TestEnabledAddr,
		BeforeHook: allowlisttest.SetDefaultRoles(nativeminter.Module.Address),
		Config: &nativeminter.Config{
			InitialMint: map[common.Address]*math.HexOrDecimal256{
				allowlisttest.TestEnabledAddr: math.NewHexOrDecimal256(2),
			},
		},
		AfterHook: func(t testing.TB, stateDB *extstate.StateDB) {
			expected := uint256.MustFromBig(common.Big2)
			require.Equal(t, expected, stateDB.GetBalance(allowlisttest.TestEnabledAddr), "expected minted funds")
		},
	},
	{
		Name:       "calling_mintNativeCoin_from_Manager_should_succeed",
		Caller:     allowlisttest.TestManagerAddr,
		BeforeHook: allowlisttest.SetDefaultRoles(nativeminter.Module.Address),
		InputFn: func(t testing.TB) []byte {
			input, err := nativeminter.PackMintNativeCoin(allowlisttest.TestEnabledAddr, common.Big1)
			require.NoError(t, err)

			return input
		},
		SuppliedGas: nativeminter.MintGasCost + nativeminter.NativeCoinMintedEventGasCost,
		ReadOnly:    false,
		ExpectedRes: []byte{},
		AfterHook: func(t testing.TB, stateDB *extstate.StateDB) {
			expected := uint256.MustFromBig(common.Big1)
			require.Equal(t, expected, stateDB.GetBalance(allowlisttest.TestEnabledAddr), "expected minted funds")

			logs := stateDB.Logs()
			assertNativeCoinMintedEvent(t, logs, allowlisttest.TestManagerAddr, allowlisttest.TestEnabledAddr, common.Big1)
		},
	},
	{
		Name:       "mint_funds_from_admin_address",
		Caller:     allowlisttest.TestAdminAddr,
		BeforeHook: allowlisttest.SetDefaultRoles(nativeminter.Module.Address),
		InputFn: func(t testing.TB) []byte {
			input, err := nativeminter.PackMintNativeCoin(allowlisttest.TestAdminAddr, common.Big1)
			require.NoError(t, err)

			return input
		},
		SuppliedGas: nativeminter.MintGasCost + nativeminter.NativeCoinMintedEventGasCost,
		ReadOnly:    false,
		ExpectedRes: []byte{},
		AfterHook: func(t testing.TB, stateDB *extstate.StateDB) {
			expected := uint256.MustFromBig(common.Big1)
			require.Equal(t, expected, stateDB.GetBalance(allowlisttest.TestAdminAddr), "expected minted funds")

			logs := stateDB.Logs()
			assertNativeCoinMintedEvent(t, logs, allowlisttest.TestAdminAddr, allowlisttest.TestAdminAddr, common.Big1)
		},
	},
	{
		Name:       "mint_max_big_funds",
		Caller:     allowlisttest.TestAdminAddr,
		BeforeHook: allowlisttest.SetDefaultRoles(nativeminter.Module.Address),
		InputFn: func(t testing.TB) []byte {
			input, err := nativeminter.PackMintNativeCoin(allowlisttest.TestAdminAddr, math.MaxBig256)
			require.NoError(t, err)

			return input
		},
		SuppliedGas: nativeminter.MintGasCost + nativeminter.NativeCoinMintedEventGasCost,
		ReadOnly:    false,
		ExpectedRes: []byte{},
		AfterHook: func(t testing.TB, stateDB *extstate.StateDB) {
			expected := uint256.MustFromBig(math.MaxBig256)
			require.Equal(t, expected, stateDB.GetBalance(allowlisttest.TestAdminAddr), "expected minted funds")

			logs := stateDB.Logs()
			assertNativeCoinMintedEvent(t, logs, allowlisttest.TestAdminAddr, allowlisttest.TestAdminAddr, math.MaxBig256)
		},
	},
	{
		Name:       "readOnly_mint_with_noRole_fails",
		Caller:     allowlisttest.TestNoRoleAddr,
		BeforeHook: allowlisttest.SetDefaultRoles(nativeminter.Module.Address),
		InputFn: func(t testing.TB) []byte {
			input, err := nativeminter.PackMintNativeCoin(allowlisttest.TestAdminAddr, common.Big1)
			require.NoError(t, err)

			return input
		},
		SuppliedGas: nativeminter.MintGasCost,
		ReadOnly:    true,
		ExpectedErr: vm.ErrWriteProtection,
	},
	{
		Name:       "readOnly_mint_with_allow_role_fails",
		Caller:     allowlisttest.TestEnabledAddr,
		BeforeHook: allowlisttest.SetDefaultRoles(nativeminter.Module.Address),
		InputFn: func(t testing.TB) []byte {
			input, err := nativeminter.PackMintNativeCoin(allowlisttest.TestEnabledAddr, common.Big1)
			require.NoError(t, err)

			return input
		},
		SuppliedGas: nativeminter.MintGasCost,
		ReadOnly:    true,
		ExpectedErr: vm.ErrWriteProtection,
	},
	{
		Name:       "readOnly_mint_with_admin_role_fails",
		Caller:     allowlisttest.TestAdminAddr,
		BeforeHook: allowlisttest.SetDefaultRoles(nativeminter.Module.Address),
		InputFn: func(t testing.TB) []byte {
			input, err := nativeminter.PackMintNativeCoin(allowlisttest.TestAdminAddr, common.Big1)
			require.NoError(t, err)

			return input
		},
		SuppliedGas: nativeminter.MintGasCost,
		ReadOnly:    true,
		ExpectedErr: vm.ErrWriteProtection,
	},
	{
		Name:       "insufficient_gas_mint_from_admin",
		Caller:     allowlisttest.TestAdminAddr,
		BeforeHook: allowlisttest.SetDefaultRoles(nativeminter.Module.Address),
		InputFn: func(t testing.TB) []byte {
			input, err := nativeminter.PackMintNativeCoin(allowlisttest.TestEnabledAddr, common.Big1)
			require.NoError(t, err)

			return input
		},
		SuppliedGas: nativeminter.MintGasCost + nativeminter.NativeCoinMintedEventGasCost - 1,
		ReadOnly:    false,
		ExpectedErr: vm.ErrOutOfGas,
	},
	{
		Name:       "mint_does_not_log_pre_Durango",
		Caller:     allowlisttest.TestEnabledAddr,
		BeforeHook: allowlisttest.SetDefaultRoles(nativeminter.Module.Address),
		ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
			config := precompileconfig.NewMockChainConfig(ctrl)
			config.EXPECT().IsDurango(gomock.Any()).Return(false).AnyTimes()
			return config
		},
		InputFn: func(t testing.TB) []byte {
			input, err := nativeminter.PackMintNativeCoin(allowlisttest.TestEnabledAddr, common.Big1)
			require.NoError(t, err)
			return input
		},
		SuppliedGas: nativeminter.MintGasCost,
		ReadOnly:    false,
		ExpectedRes: []byte{},
		AfterHook: func(t testing.TB, stateDB *extstate.StateDB) {
			// Check no logs are stored in state
			logs := stateDB.Logs()
			require.Empty(t, logs)
		},
	},
	{
		Name:       "mint_with_extra_padded_bytes_should_fail_pre_Durango",
		Caller:     allowlisttest.TestEnabledAddr,
		BeforeHook: allowlisttest.SetDefaultRoles(nativeminter.Module.Address),
		ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
			config := precompileconfig.NewMockChainConfig(ctrl)
			config.EXPECT().IsDurango(gomock.Any()).Return(false).AnyTimes()
			return config
		},
		InputFn: func(t testing.TB) []byte {
			input, err := nativeminter.PackMintNativeCoin(allowlisttest.TestEnabledAddr, common.Big1)
			require.NoError(t, err)

			// Add extra bytes to the end of the input
			input = append(input, make([]byte, 32)...)

			return input
		},
		SuppliedGas: nativeminter.MintGasCost,
		ReadOnly:    false,
		ExpectedErr: nativeminter.ErrInvalidLen,
	},
	{
		Name:       "mint_with_extra_padded_bytes_should_succeed_with_Durango",
		Caller:     allowlisttest.TestEnabledAddr,
		BeforeHook: allowlisttest.SetDefaultRoles(nativeminter.Module.Address),
		ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
			config := precompileconfig.NewMockChainConfig(ctrl)
			config.EXPECT().IsDurango(gomock.Any()).Return(true).AnyTimes()
			return config
		},
		InputFn: func(t testing.TB) []byte {
			input, err := nativeminter.PackMintNativeCoin(allowlisttest.TestEnabledAddr, common.Big1)
			require.NoError(t, err)

			// Add extra bytes to the end of the input
			input = append(input, make([]byte, 32)...)

			return input
		},
		ExpectedRes: []byte{},
		SuppliedGas: nativeminter.MintGasCost + nativeminter.NativeCoinMintedEventGasCost,
		ReadOnly:    false,
		AfterHook: func(t testing.TB, state *extstate.StateDB) {
			expected := uint256.MustFromBig(common.Big1)
			require.Equal(t, expected, state.GetBalance(allowlisttest.TestEnabledAddr), "expected minted funds")

			logs := state.Logs()
			assertNativeCoinMintedEvent(t, logs, allowlisttest.TestEnabledAddr, allowlisttest.TestEnabledAddr, common.Big1)
		},
	},
}

func TestContractNativeMinterRun(t *testing.T) {
	allowlisttest.RunPrecompileWithAllowListTests(t, nativeminter.Module, tests)
}

func assertNativeCoinMintedEvent(t testing.TB,
	logs []*ethtypes.Log,
	expectedSender common.Address,
	expectedRecipient common.Address,
	expectedAmount *big.Int,
) {
	require.Len(t, logs, 1)
	log := logs[0]
	require.Equal(
		t,
		[]common.Hash{
			nativeminter.NativeMinterABI.Events["NativeCoinMinted"].ID,
			common.BytesToHash(expectedSender[:]),
			common.BytesToHash(expectedRecipient[:]),
		},
		log.Topics,
	)
	require.NotEmpty(t, log.Data)
	amount, err := nativeminter.UnpackNativeCoinMintedEventData(log.Data)
	require.NoError(t, err)
	require.Zero(t, expectedAmount.Cmp(amount), "expected", expectedAmount, "got", amount)
}
