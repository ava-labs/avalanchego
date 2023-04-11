// (c) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package allowlist

import (
	"testing"

	"github.com/ava-labs/subnet-evm/precompile/contract"
	"github.com/ava-labs/subnet-evm/precompile/modules"
	"github.com/ava-labs/subnet-evm/precompile/testutils"
	"github.com/ava-labs/subnet-evm/vmerrs"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

var (
	TestAdminAddr   = common.HexToAddress("0x0000000000000000000000000000000000000011")
	TestEnabledAddr = common.HexToAddress("0x0000000000000000000000000000000000000022")
	TestNoRoleAddr  = common.HexToAddress("0x0000000000000000000000000000000000000033")
)

func AllowListTests(module modules.Module) map[string]testutils.PrecompileTest {
	contractAddress := module.Address
	return map[string]testutils.PrecompileTest{
		"set admin": {
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestNoRoleAddr, AdminRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state contract.StateDB) {
				res := GetAllowListStatus(state, contractAddress, TestNoRoleAddr)
				require.Equal(t, AdminRole, res)
			},
		},
		"set enabled": {
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestNoRoleAddr, EnabledRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state contract.StateDB) {
				res := GetAllowListStatus(state, contractAddress, TestNoRoleAddr)
				require.Equal(t, EnabledRole, res)
			},
		},
		"set no role": {
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestEnabledAddr, NoRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state contract.StateDB) {
				res := GetAllowListStatus(state, contractAddress, TestEnabledAddr)
				require.Equal(t, NoRole, res)
			},
		},
		"set no role from no role": {
			Caller:     TestNoRoleAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestEnabledAddr, NoRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: ErrCannotModifyAllowList.Error(),
		},
		"set enabled from no role": {
			Caller:     TestNoRoleAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestNoRoleAddr, EnabledRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: ErrCannotModifyAllowList.Error(),
		},
		"set admin from no role": {
			Caller:     TestNoRoleAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestEnabledAddr, AdminRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: ErrCannotModifyAllowList.Error(),
		},
		"set no role from enabled": {
			Caller:     TestEnabledAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestAdminAddr, NoRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: ErrCannotModifyAllowList.Error(),
		},
		"set enabled from enabled": {
			Caller:     TestEnabledAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestNoRoleAddr, EnabledRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: ErrCannotModifyAllowList.Error(),
		},
		"set admin from enabled": {
			Caller:     TestEnabledAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestNoRoleAddr, AdminRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: ErrCannotModifyAllowList.Error(),
		},
		"set no role with readOnly enabled": {
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestEnabledAddr, NoRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ModifyAllowListGasCost,
			ReadOnly:    true,
			ExpectedErr: vmerrs.ErrWriteProtection.Error(),
		},
		"set no role insufficient gas": {
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestEnabledAddr, NoRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ModifyAllowListGasCost - 1,
			ReadOnly:    false,
			ExpectedErr: vmerrs.ErrOutOfGas.Error(),
		},
		"read allow list no role": {
			Caller:      TestNoRoleAddr,
			BeforeHook:  SetDefaultRoles(contractAddress),
			Input:       PackReadAllowList(TestNoRoleAddr),
			SuppliedGas: ReadAllowListGasCost,
			ReadOnly:    false,
			ExpectedRes: common.Hash(NoRole).Bytes(),
		},
		"read allow list admin role": {
			Caller:      TestAdminAddr,
			BeforeHook:  SetDefaultRoles(contractAddress),
			Input:       PackReadAllowList(TestAdminAddr),
			SuppliedGas: ReadAllowListGasCost,
			ReadOnly:    false,
			ExpectedRes: common.Hash(AdminRole).Bytes(),
		},
		"read allow list with readOnly enabled": {
			Caller:      TestAdminAddr,
			BeforeHook:  SetDefaultRoles(contractAddress),
			Input:       PackReadAllowList(TestNoRoleAddr),
			SuppliedGas: ReadAllowListGasCost,
			ReadOnly:    true,
			ExpectedRes: common.Hash(NoRole).Bytes(),
		},
		"read allow list out of gas": {
			Caller:      TestAdminAddr,
			BeforeHook:  SetDefaultRoles(contractAddress),
			Input:       PackReadAllowList(TestNoRoleAddr),
			SuppliedGas: ReadAllowListGasCost - 1,
			ReadOnly:    true,
			ExpectedErr: vmerrs.ErrOutOfGas.Error(),
		},
		"initial config sets admins": {
			Config: mkConfigWithAllowList(
				module,
				&AllowListConfig{
					AdminAddresses: []common.Address{TestNoRoleAddr, TestEnabledAddr},
				},
			),
			SuppliedGas: 0,
			ReadOnly:    false,
			AfterHook: func(t testing.TB, state contract.StateDB) {
				require.Equal(t, AdminRole, GetAllowListStatus(state, contractAddress, TestNoRoleAddr))
				require.Equal(t, AdminRole, GetAllowListStatus(state, contractAddress, TestEnabledAddr))
			},
		},
		"initial config sets enabled": {
			Config: mkConfigWithAllowList(
				module,
				&AllowListConfig{
					EnabledAddresses: []common.Address{TestNoRoleAddr, TestAdminAddr},
				},
			),
			SuppliedGas: 0,
			ReadOnly:    false,
			AfterHook: func(t testing.TB, state contract.StateDB) {
				require.Equal(t, EnabledRole, GetAllowListStatus(state, contractAddress, TestAdminAddr))
				require.Equal(t, EnabledRole, GetAllowListStatus(state, contractAddress, TestNoRoleAddr))
			},
		},
	}
}

// SetDefaultRoles returns a BeforeHook that sets roles TestAdminAddr and TestEnabledAddr
// to have the AdminRole and EnabledRole respectively.
func SetDefaultRoles(contractAddress common.Address) func(t testing.TB, state contract.StateDB) {
	return func(t testing.TB, state contract.StateDB) {
		SetAllowListRole(state, contractAddress, TestAdminAddr, AdminRole)
		SetAllowListRole(state, contractAddress, TestEnabledAddr, EnabledRole)
		require.Equal(t, AdminRole, GetAllowListStatus(state, contractAddress, TestAdminAddr))
		require.Equal(t, EnabledRole, GetAllowListStatus(state, contractAddress, TestEnabledAddr))
		require.Equal(t, NoRole, GetAllowListStatus(state, contractAddress, TestNoRoleAddr))
	}
}

func RunPrecompileWithAllowListTests(t *testing.T, module modules.Module, newStateDB func(t testing.TB) contract.StateDB, contractTests map[string]testutils.PrecompileTest) {
	t.Helper()
	tests := AllowListTests(module)
	// Add the contract specific tests to the map of tests to run.
	for name, test := range contractTests {
		if _, exists := tests[name]; exists {
			t.Fatalf("duplicate test name: %s", name)
		}
		tests[name] = test
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			test.Run(t, module, newStateDB(t))
		})
	}
}

func BenchPrecompileWithAllowList(b *testing.B, module modules.Module, newStateDB func(t testing.TB) contract.StateDB, contractTests map[string]testutils.PrecompileTest) {
	b.Helper()

	tests := AllowListTests(module)
	// Add the contract specific tests to the map of tests to run.
	for name, test := range contractTests {
		if _, exists := tests[name]; exists {
			b.Fatalf("duplicate bench name: %s", name)
		}
		tests[name] = test
	}

	for name, test := range tests {
		b.Run(name, func(b *testing.B) {
			test.Bench(b, module, newStateDB(b))
		})
	}
}
