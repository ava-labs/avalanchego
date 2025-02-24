// (c) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nativeminter

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/math"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/subnet-evm/core/extstate"
	"github.com/ava-labs/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/subnet-evm/precompile/contract"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/subnet-evm/precompile/testutils"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

var (
	tests = map[string]testutils.PrecompileTest{
		"calling mintNativeCoin from NoRole should fail": {
			Caller:     allowlist.TestNoRoleAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackMintNativeCoin(allowlist.TestNoRoleAddr, common.Big1)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: MintGasCost,
			ReadOnly:    false,
			ExpectedErr: ErrCannotMint.Error(),
		},
		"calling mintNativeCoin from Enabled should succeed": {
			Caller:     allowlist.TestEnabledAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackMintNativeCoin(allowlist.TestEnabledAddr, common.Big1)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: MintGasCost + NativeCoinMintedEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, stateDB contract.StateDB) {
				expected := uint256.MustFromBig(common.Big1)
				require.Equal(t, expected, stateDB.GetBalance(allowlist.TestEnabledAddr), "expected minted funds")

				logsTopics, logsData := stateDB.GetLogData()
				assertNativeCoinMintedEvent(t, logsTopics, logsData, allowlist.TestEnabledAddr, allowlist.TestEnabledAddr, common.Big1)
			},
		},
		"initial mint funds": {
			Caller:     allowlist.TestEnabledAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			Config: &Config{
				InitialMint: map[common.Address]*math.HexOrDecimal256{
					allowlist.TestEnabledAddr: math.NewHexOrDecimal256(2),
				},
			},
			AfterHook: func(t testing.TB, stateDB contract.StateDB) {
				expected := uint256.MustFromBig(common.Big2)
				require.Equal(t, expected, stateDB.GetBalance(allowlist.TestEnabledAddr), "expected minted funds")
			},
		},
		"calling mintNativeCoin from Manager should succeed": {
			Caller:     allowlist.TestManagerAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackMintNativeCoin(allowlist.TestEnabledAddr, common.Big1)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: MintGasCost + NativeCoinMintedEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, stateDB contract.StateDB) {
				expected := uint256.MustFromBig(common.Big1)
				require.Equal(t, expected, stateDB.GetBalance(allowlist.TestEnabledAddr), "expected minted funds")

				logsTopics, logsData := stateDB.GetLogData()
				assertNativeCoinMintedEvent(t, logsTopics, logsData, allowlist.TestManagerAddr, allowlist.TestEnabledAddr, common.Big1)
			},
		},
		"mint funds from admin address": {
			Caller:     allowlist.TestAdminAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackMintNativeCoin(allowlist.TestAdminAddr, common.Big1)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: MintGasCost + NativeCoinMintedEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, stateDB contract.StateDB) {
				expected := uint256.MustFromBig(common.Big1)
				require.Equal(t, expected, stateDB.GetBalance(allowlist.TestAdminAddr), "expected minted funds")

				logsTopics, logsData := stateDB.GetLogData()
				assertNativeCoinMintedEvent(t, logsTopics, logsData, allowlist.TestAdminAddr, allowlist.TestAdminAddr, common.Big1)
			},
		},
		"mint max big funds": {
			Caller:     allowlist.TestAdminAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackMintNativeCoin(allowlist.TestAdminAddr, math.MaxBig256)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: MintGasCost + NativeCoinMintedEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, stateDB contract.StateDB) {
				expected := uint256.MustFromBig(math.MaxBig256)
				require.Equal(t, expected, stateDB.GetBalance(allowlist.TestAdminAddr), "expected minted funds")

				logsTopics, logsData := stateDB.GetLogData()
				assertNativeCoinMintedEvent(t, logsTopics, logsData, allowlist.TestAdminAddr, allowlist.TestAdminAddr, math.MaxBig256)
			},
		},
		"readOnly mint with noRole fails": {
			Caller:     allowlist.TestNoRoleAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackMintNativeCoin(allowlist.TestAdminAddr, common.Big1)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: MintGasCost,
			ReadOnly:    true,
			ExpectedErr: vm.ErrWriteProtection.Error(),
		},
		"readOnly mint with allow role fails": {
			Caller:     allowlist.TestEnabledAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackMintNativeCoin(allowlist.TestEnabledAddr, common.Big1)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: MintGasCost,
			ReadOnly:    true,
			ExpectedErr: vm.ErrWriteProtection.Error(),
		},
		"readOnly mint with admin role fails": {
			Caller:     allowlist.TestAdminAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackMintNativeCoin(allowlist.TestAdminAddr, common.Big1)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: MintGasCost,
			ReadOnly:    true,
			ExpectedErr: vm.ErrWriteProtection.Error(),
		},
		"insufficient gas mint from admin": {
			Caller:     allowlist.TestAdminAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			InputFn: func(t testing.TB) []byte {
				input, err := PackMintNativeCoin(allowlist.TestEnabledAddr, common.Big1)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: MintGasCost + NativeCoinMintedEventGasCost - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
		"mint doesn't log pre-Durango": {
			Caller:     allowlist.TestEnabledAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(false).AnyTimes()
				return config
			},
			InputFn: func(t testing.TB) []byte {
				input, err := PackMintNativeCoin(allowlist.TestEnabledAddr, common.Big1)
				require.NoError(t, err)
				return input
			},
			SuppliedGas: MintGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, stateDB contract.StateDB) {
				// Check no logs are stored in state
				logsTopics, logsData := stateDB.GetLogData()
				require.Len(t, logsTopics, 0)
				require.Len(t, logsData, 0)
			},
		},
		"mint with extra padded bytes should fail pre-Durango": {
			Caller:     allowlist.TestEnabledAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(false).AnyTimes()
				return config
			},
			InputFn: func(t testing.TB) []byte {
				input, err := PackMintNativeCoin(allowlist.TestEnabledAddr, common.Big1)
				require.NoError(t, err)

				// Add extra bytes to the end of the input
				input = append(input, make([]byte, 32)...)

				return input
			},
			SuppliedGas: MintGasCost,
			ReadOnly:    false,
			ExpectedErr: ErrInvalidLen.Error(),
		},
		"mint with extra padded bytes should succeed with Durango": {
			Caller:     allowlist.TestEnabledAddr,
			BeforeHook: allowlist.SetDefaultRoles(Module.Address),
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(true).AnyTimes()
				return config
			},
			InputFn: func(t testing.TB) []byte {
				input, err := PackMintNativeCoin(allowlist.TestEnabledAddr, common.Big1)
				require.NoError(t, err)

				// Add extra bytes to the end of the input
				input = append(input, make([]byte, 32)...)

				return input
			},
			ExpectedRes: []byte{},
			SuppliedGas: MintGasCost + NativeCoinMintedEventGasCost,
			ReadOnly:    false,
			AfterHook: func(t testing.TB, state contract.StateDB) {
				expected := uint256.MustFromBig(common.Big1)
				require.Equal(t, expected, state.GetBalance(allowlist.TestEnabledAddr), "expected minted funds")

				logsTopics, logsData := state.GetLogData()
				assertNativeCoinMintedEvent(t, logsTopics, logsData, allowlist.TestEnabledAddr, allowlist.TestEnabledAddr, common.Big1)
			},
		},
	}
)

func TestContractNativeMinterRun(t *testing.T) {
	allowlist.RunPrecompileWithAllowListTests(t, Module, extstate.NewTestStateDB, tests)
}

func BenchmarkContractNativeMinter(b *testing.B) {
	allowlist.BenchPrecompileWithAllowList(b, Module, extstate.NewTestStateDB, tests)
}

func assertNativeCoinMintedEvent(t testing.TB,
	logsTopics [][]common.Hash,
	logsData [][]byte,
	expectedSender common.Address,
	expectedRecipient common.Address,
	expectedAmount *big.Int,
) {
	require.Len(t, logsTopics, 1)
	require.Len(t, logsData, 1)
	topics := logsTopics[0]
	require.Len(t, topics, 3)
	require.Equal(t, NativeMinterABI.Events["NativeCoinMinted"].ID, topics[0])
	require.Equal(t, common.BytesToHash(expectedSender[:]), topics[1])
	require.Equal(t, common.BytesToHash(expectedRecipient[:]), topics[2])
	require.NotEmpty(t, logsData[0])
	amount, err := UnpackNativeCoinMintedEventData(logsData[0])
	require.NoError(t, err)
	require.True(t, expectedAmount.Cmp(amount) == 0, "expected", expectedAmount, "got", amount)
}
