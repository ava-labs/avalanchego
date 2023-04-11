// (c) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nativeminter

import (
	"testing"

	"github.com/ava-labs/subnet-evm/core/state"
	"github.com/ava-labs/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/subnet-evm/precompile/contract"
	"github.com/ava-labs/subnet-evm/precompile/testutils"
	"github.com/ava-labs/subnet-evm/vmerrs"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/stretchr/testify/require"
)

var tests = map[string]testutils.PrecompileTest{
	"mint funds from no role fails": {
		Caller:     allowlist.TestNoRoleAddr,
		BeforeHook: allowlist.SetDefaultRoles(Module.Address),
		InputFn: func(t testing.TB) []byte {
			input, err := PackMintInput(allowlist.TestNoRoleAddr, common.Big1)
			require.NoError(t, err)

			return input
		},
		SuppliedGas: MintGasCost,
		ReadOnly:    false,
		ExpectedErr: ErrCannotMint.Error(),
	},
	"mint funds from enabled address": {
		Caller:     allowlist.TestEnabledAddr,
		BeforeHook: allowlist.SetDefaultRoles(Module.Address),
		InputFn: func(t testing.TB) []byte {
			input, err := PackMintInput(allowlist.TestEnabledAddr, common.Big1)
			require.NoError(t, err)

			return input
		},
		SuppliedGas: MintGasCost,
		ReadOnly:    false,
		ExpectedRes: []byte{},
		AfterHook: func(t testing.TB, state contract.StateDB) {
			require.Equal(t, common.Big1, state.GetBalance(allowlist.TestEnabledAddr), "expected minted funds")
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
		AfterHook: func(t testing.TB, state contract.StateDB) {
			require.Equal(t, common.Big2, state.GetBalance(allowlist.TestEnabledAddr), "expected minted funds")
		},
	},
	"mint funds from admin address": {
		Caller:     allowlist.TestAdminAddr,
		BeforeHook: allowlist.SetDefaultRoles(Module.Address),
		InputFn: func(t testing.TB) []byte {
			input, err := PackMintInput(allowlist.TestAdminAddr, common.Big1)
			require.NoError(t, err)

			return input
		},
		SuppliedGas: MintGasCost,
		ReadOnly:    false,
		ExpectedRes: []byte{},
		AfterHook: func(t testing.TB, state contract.StateDB) {
			require.Equal(t, common.Big1, state.GetBalance(allowlist.TestAdminAddr), "expected minted funds")
		},
	},
	"mint max big funds": {
		Caller:     allowlist.TestAdminAddr,
		BeforeHook: allowlist.SetDefaultRoles(Module.Address),
		InputFn: func(t testing.TB) []byte {
			input, err := PackMintInput(allowlist.TestAdminAddr, math.MaxBig256)
			require.NoError(t, err)

			return input
		},
		SuppliedGas: MintGasCost,
		ReadOnly:    false,
		ExpectedRes: []byte{},
		AfterHook: func(t testing.TB, state contract.StateDB) {
			require.Equal(t, math.MaxBig256, state.GetBalance(allowlist.TestAdminAddr), "expected minted funds")
		},
	},
	"readOnly mint with noRole fails": {
		Caller:     allowlist.TestNoRoleAddr,
		BeforeHook: allowlist.SetDefaultRoles(Module.Address),
		InputFn: func(t testing.TB) []byte {
			input, err := PackMintInput(allowlist.TestAdminAddr, common.Big1)
			require.NoError(t, err)

			return input
		},
		SuppliedGas: MintGasCost,
		ReadOnly:    true,
		ExpectedErr: vmerrs.ErrWriteProtection.Error(),
	},
	"readOnly mint with allow role fails": {
		Caller:     allowlist.TestEnabledAddr,
		BeforeHook: allowlist.SetDefaultRoles(Module.Address),
		InputFn: func(t testing.TB) []byte {
			input, err := PackMintInput(allowlist.TestEnabledAddr, common.Big1)
			require.NoError(t, err)

			return input
		},
		SuppliedGas: MintGasCost,
		ReadOnly:    true,
		ExpectedErr: vmerrs.ErrWriteProtection.Error(),
	},
	"readOnly mint with admin role fails": {
		Caller:     allowlist.TestAdminAddr,
		BeforeHook: allowlist.SetDefaultRoles(Module.Address),
		InputFn: func(t testing.TB) []byte {
			input, err := PackMintInput(allowlist.TestAdminAddr, common.Big1)
			require.NoError(t, err)

			return input
		},
		SuppliedGas: MintGasCost,
		ReadOnly:    true,
		ExpectedErr: vmerrs.ErrWriteProtection.Error(),
	},
	"insufficient gas mint from admin": {
		Caller:     allowlist.TestAdminAddr,
		BeforeHook: allowlist.SetDefaultRoles(Module.Address),
		InputFn: func(t testing.TB) []byte {
			input, err := PackMintInput(allowlist.TestEnabledAddr, common.Big1)
			require.NoError(t, err)

			return input
		},
		SuppliedGas: MintGasCost - 1,
		ReadOnly:    false,
		ExpectedErr: vmerrs.ErrOutOfGas.Error(),
	},
}

func TestContractNativeMinterRun(t *testing.T) {
	allowlist.RunPrecompileWithAllowListTests(t, Module, state.NewTestStateDB, tests)
}

func BenchmarkContractNativeMinter(b *testing.B) {
	allowlist.BenchPrecompileWithAllowList(b, Module, state.NewTestStateDB, tests)
}
