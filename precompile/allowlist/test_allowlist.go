// (c) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package allowlist

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/subnet-evm/precompile/contract"
	"github.com/ava-labs/subnet-evm/precompile/modules"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/subnet-evm/precompile/testutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

var (
	TestAdminAddr   = common.HexToAddress("0x0000000000000000000000000000000000000011")
	TestEnabledAddr = common.HexToAddress("0x0000000000000000000000000000000000000022")
	TestNoRoleAddr  = common.HexToAddress("0x0000000000000000000000000000000000000033")
	TestManagerAddr = common.HexToAddress("0x0000000000000000000000000000000000000044")
)

func AllowListTests(t testing.TB, module modules.Module) map[string]testutils.PrecompileTest {
	contractAddress := module.Address
	return map[string]testutils.PrecompileTest{
		"admin set admin": {
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestNoRoleAddr, AdminRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ModifyAllowListGasCost + AllowListEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state contract.StateDB) {
				res := GetAllowListStatus(state, contractAddress, TestNoRoleAddr)
				require.Equal(t, AdminRole, res)
				// Check logs are stored in state
				logsTopics, logsData := state.GetLogData()
				assertSetRoleEvent(t, logsTopics, logsData, AdminRole, TestNoRoleAddr, TestAdminAddr, NoRole)
			},
		},
		"admin set enabled": {
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestNoRoleAddr, EnabledRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ModifyAllowListGasCost + AllowListEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state contract.StateDB) {
				res := GetAllowListStatus(state, contractAddress, TestNoRoleAddr)
				require.Equal(t, EnabledRole, res)
				// Check logs are stored in state
				logsTopics, logsData := state.GetLogData()
				assertSetRoleEvent(t, logsTopics, logsData, EnabledRole, TestNoRoleAddr, TestAdminAddr, NoRole)
			},
		},
		"admin set no role": {
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestEnabledAddr, NoRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ModifyAllowListGasCost + AllowListEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state contract.StateDB) {
				res := GetAllowListStatus(state, contractAddress, TestEnabledAddr)
				require.Equal(t, NoRole, res)
				// Check logs are stored in state
				logsTopics, logsData := state.GetLogData()
				assertSetRoleEvent(t, logsTopics, logsData, NoRole, TestEnabledAddr, TestAdminAddr, EnabledRole)
			},
		},
		"no role set no role": {
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
		"no role set enabled": {
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
		"no role set admin": {
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
		"enabled set no role": {
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
		"enabled set enabled": {
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
		"enabled set admin": {
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
		"no role set manager pre-Durango": {
			Caller:     TestNoRoleAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(false).AnyTimes()
				return config
			},
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestNoRoleAddr, ManagerRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: 0,
			ReadOnly:    false,
			ExpectedErr: "invalid non-activated function selector",
		},
		"no role set manager": {
			Caller:     TestNoRoleAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(true).AnyTimes()
				return config
			},
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestNoRoleAddr, ManagerRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: ErrCannotModifyAllowList.Error(),
		},
		"enabled role set manager pre-Durango": {
			Caller:     TestEnabledAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(false).AnyTimes()
				return config
			},
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestNoRoleAddr, ManagerRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: 0,
			ReadOnly:    false,
			ExpectedErr: "invalid non-activated function selector",
		},
		"enabled set manager": {
			Caller:     TestNoRoleAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(true).AnyTimes()
				return config
			},
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestNoRoleAddr, ManagerRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: ErrCannotModifyAllowList.Error(),
		},
		"admin set manager pre-DUpgarde": {
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestNoRoleAddr, ManagerRole)
				require.NoError(t, err)

				return input
			},
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(false).AnyTimes()
				return config
			},
			SuppliedGas: 0,
			ReadOnly:    false,
			ExpectedErr: "invalid non-activated function selector",
		},
		"admin set manager": {
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestNoRoleAddr, ManagerRole)
				require.NoError(t, err)

				return input
			},
			ExpectedRes: []byte{},
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(true).AnyTimes()
				return config
			},
			SuppliedGas: ModifyAllowListGasCost + AllowListEventGasCost,
			ReadOnly:    false,
			AfterHook: func(t testing.TB, state contract.StateDB) {
				res := GetAllowListStatus(state, contractAddress, TestNoRoleAddr)
				require.Equal(t, ManagerRole, res)
				// Check logs are stored in state
				logsTopics, logsData := state.GetLogData()
				assertSetRoleEvent(t, logsTopics, logsData, ManagerRole, TestNoRoleAddr, TestAdminAddr, NoRole)
			},
		},
		"manager set no role to no role": {
			Caller:     TestManagerAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestNoRoleAddr, NoRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ModifyAllowListGasCost + AllowListEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			ExpectedErr: "",
			AfterHook: func(t testing.TB, state contract.StateDB) {
				res := GetAllowListStatus(state, contractAddress, TestNoRoleAddr)
				require.Equal(t, NoRole, res)
				// Check logs are stored in state
				logsTopics, logsData := state.GetLogData()
				assertSetRoleEvent(t, logsTopics, logsData, NoRole, TestNoRoleAddr, TestManagerAddr, NoRole)
			},
		},
		"manager set no role to enabled": {
			Caller:     TestManagerAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestNoRoleAddr, EnabledRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ModifyAllowListGasCost + AllowListEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			ExpectedErr: "",
			AfterHook: func(t testing.TB, state contract.StateDB) {
				res := GetAllowListStatus(state, contractAddress, TestNoRoleAddr)
				require.Equal(t, EnabledRole, res)

				// Check logs are stored in state
				logsTopics, logsData := state.GetLogData()
				assertSetRoleEvent(t, logsTopics, logsData, EnabledRole, TestNoRoleAddr, TestManagerAddr, NoRole)
			},
		},
		"manager set no role to manager": {
			Caller:     TestManagerAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(true).AnyTimes()
				return config
			},
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestNoRoleAddr, ManagerRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: ErrCannotModifyAllowList.Error(),
		},
		"manager set no role to admin": {
			Caller:     TestManagerAddr,
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
		"manager set enabled to admin": {
			Caller:     TestManagerAddr,
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
		"manager set enabled role to manager": {
			Caller:     TestManagerAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(true).AnyTimes()
				return config
			},
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestEnabledAddr, ManagerRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: ErrCannotModifyAllowList.Error(),
		},
		"manager set enabled role to no role": {
			Caller:     TestManagerAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestEnabledAddr, NoRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ModifyAllowListGasCost + AllowListEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state contract.StateDB) {
				res := GetAllowListStatus(state, contractAddress, TestNoRoleAddr)
				require.Equal(t, NoRole, res)

				// Check logs are stored in state
				logsTopics, logsData := state.GetLogData()
				assertSetRoleEvent(t, logsTopics, logsData, NoRole, TestEnabledAddr, TestManagerAddr, EnabledRole)
			},
		},
		"manager set admin to no role": {
			Caller:     TestManagerAddr,
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
		"manager set admin role to enabled": {
			Caller:     TestManagerAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestAdminAddr, EnabledRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: ErrCannotModifyAllowList.Error(),
		},
		"manager set admin to manager": {
			Caller:     TestManagerAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(true).AnyTimes()
				return config
			},
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestAdminAddr, ManagerRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: ErrCannotModifyAllowList.Error(),
		},
		"manager set manager to no role": {
			Caller:     TestManagerAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestManagerAddr, NoRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: ErrCannotModifyAllowList.Error(),
		},
		"admin set no role with readOnly enabled": {
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestEnabledAddr, NoRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ModifyAllowListGasCost,
			ReadOnly:    true,
			ExpectedErr: vm.ErrWriteProtection.Error(),
		},
		"admin set no role insufficient gas": {
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestEnabledAddr, NoRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ModifyAllowListGasCost - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
		"no role read allow list": {
			Caller:     TestNoRoleAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackReadAllowList(TestNoRoleAddr)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: ReadAllowListGasCost,
			ReadOnly:    false,
			ExpectedRes: common.Hash(NoRole).Bytes(),
		},
		"admin role read allow list": {
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackReadAllowList(TestAdminAddr)
				require.NoError(t, err)

				return input
			}, SuppliedGas: ReadAllowListGasCost,
			ReadOnly:    false,
			ExpectedRes: common.Hash(AdminRole).Bytes(),
		},
		"admin read allow list with readOnly enabled": {
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackReadAllowList(TestNoRoleAddr)
				require.NoError(t, err)

				return input
			}, SuppliedGas: ReadAllowListGasCost,
			ReadOnly:    true,
			ExpectedRes: common.Hash(NoRole).Bytes(),
		},
		"radmin read allow list out of gas": {
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := PackReadAllowList(TestNoRoleAddr)
				require.NoError(t, err)

				return input
			}, SuppliedGas: ReadAllowListGasCost - 1,
			ReadOnly:    true,
			ExpectedErr: vm.ErrOutOfGas.Error(),
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
		"initial config sets managers": {
			Config: mkConfigWithAllowList(
				module,
				&AllowListConfig{
					ManagerAddresses: []common.Address{TestNoRoleAddr, TestEnabledAddr},
				},
			),
			SuppliedGas: 0,
			ReadOnly:    false,
			AfterHook: func(t testing.TB, state contract.StateDB) {
				require.Equal(t, ManagerRole, GetAllowListStatus(state, contractAddress, TestNoRoleAddr))
				require.Equal(t, ManagerRole, GetAllowListStatus(state, contractAddress, TestEnabledAddr))
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
		"admin set admin pre-Durango": {
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(false).AnyTimes()
				return config
			},
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestNoRoleAddr, AdminRole)
				require.NoError(t, err)
				return input
			},
			SuppliedGas: ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, stateDB contract.StateDB) {
				// Check no logs are stored in state
				topics, data := stateDB.GetLogData()
				require.Len(t, topics, 0)
				require.Len(t, data, 0)
			},
		},
		"admin set enabled pre-Durango": {
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(false).AnyTimes()
				return config
			},
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestNoRoleAddr, EnabledRole)
				require.NoError(t, err)
				return input
			},
			SuppliedGas: ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, stateDB contract.StateDB) {
				// Check no logs are stored in state
				topics, data := stateDB.GetLogData()
				require.Len(t, topics, 0)
				require.Len(t, data, 0)
			},
		},
		"admin set no role pre-Durango": {
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(false).AnyTimes()
				return config
			},
			InputFn: func(t testing.TB) []byte {
				input, err := PackModifyAllowList(TestEnabledAddr, NoRole)
				require.NoError(t, err)
				return input
			},
			SuppliedGas: ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, stateDB contract.StateDB) {
				// Check no logs are stored in state
				topics, data := stateDB.GetLogData()
				require.Len(t, topics, 0)
				require.Len(t, data, 0)
			},
		},
	}
}

// SetDefaultRoles returns a BeforeHook that sets roles TestAdminAddr and TestEnabledAddr
// to have the AdminRole and EnabledRole respectively.
func SetDefaultRoles(contractAddress common.Address) func(t testing.TB, state contract.StateDB) {
	return func(t testing.TB, state contract.StateDB) {
		SetAllowListRole(state, contractAddress, TestAdminAddr, AdminRole)
		SetAllowListRole(state, contractAddress, TestManagerAddr, ManagerRole)
		SetAllowListRole(state, contractAddress, TestEnabledAddr, EnabledRole)
		require.Equal(t, AdminRole, GetAllowListStatus(state, contractAddress, TestAdminAddr))
		require.Equal(t, ManagerRole, GetAllowListStatus(state, contractAddress, TestManagerAddr))
		require.Equal(t, EnabledRole, GetAllowListStatus(state, contractAddress, TestEnabledAddr))
		require.Equal(t, NoRole, GetAllowListStatus(state, contractAddress, TestNoRoleAddr))
	}
}

func RunPrecompileWithAllowListTests(t *testing.T, module modules.Module, newStateDB func(t testing.TB) contract.StateDB, contractTests map[string]testutils.PrecompileTest) {
	t.Helper()
	tests := AllowListTests(t, module)
	// Add the contract specific tests to the map of tests to run.
	for name, test := range contractTests {
		if _, exists := tests[name]; exists {
			t.Fatalf("duplicate test name: %s", name)
		}
		tests[name] = test
	}

	testutils.RunPrecompileTests(t, module, newStateDB, tests)
}

func BenchPrecompileWithAllowList(b *testing.B, module modules.Module, newStateDB func(t testing.TB) contract.StateDB, contractTests map[string]testutils.PrecompileTest) {
	b.Helper()

	tests := AllowListTests(b, module)
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

func assertSetRoleEvent(t testing.TB, logsTopics [][]common.Hash, logsData [][]byte, role Role, addr common.Address, caller common.Address, oldRole Role) {
	require.Len(t, logsTopics, 1)
	require.Len(t, logsData, 1)
	topics := logsTopics[0]
	require.Len(t, topics, 4)
	require.Equal(t, AllowListABI.Events["RoleSet"].ID, topics[0])
	require.Equal(t, role.Hash(), topics[1])
	require.Equal(t, common.BytesToHash(addr[:]), topics[2])
	require.Equal(t, common.BytesToHash(caller[:]), topics[3])
	data := logsData[0]
	require.Equal(t, oldRole.Bytes(), data)
}
