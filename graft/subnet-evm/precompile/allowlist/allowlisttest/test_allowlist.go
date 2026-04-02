// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package allowlisttest

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/core/extstate"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contract"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/modules"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompiletest"

	ethtypes "github.com/ava-labs/libevm/core/types"
)

var (
	TestAdminAddr   = common.HexToAddress("0x0000000000000000000000000000000000000011")
	TestEnabledAddr = common.HexToAddress("0x0000000000000000000000000000000000000022")
	TestNoRoleAddr  = common.HexToAddress("0x0000000000000000000000000000000000000033")
	TestManagerAddr = common.HexToAddress("0x0000000000000000000000000000000000000044")
)

func AllowListTests(_ testing.TB, module modules.Module) []precompiletest.PrecompileTest {
	contractAddress := module.Address
	return []precompiletest.PrecompileTest{
		{
			Name:       "admin_set_admin",
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.AdminRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost + allowlist.AllowListEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				res := allowlist.GetAllowListStatus(state, contractAddress, TestNoRoleAddr)
				require.Equal(t, allowlist.AdminRole, res)
				// Check logs are stored in state
				logs := state.Logs()
				assertSetRoleEvent(t, logs, allowlist.AdminRole, TestNoRoleAddr, TestAdminAddr, allowlist.NoRole)
			},
		},
		{
			Name:       "admin_set_enabled",
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.EnabledRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost + allowlist.AllowListEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				res := allowlist.GetAllowListStatus(state, contractAddress, TestNoRoleAddr)
				require.Equal(t, allowlist.EnabledRole, res)
				// Check logs are stored in state
				logs := state.Logs()
				assertSetRoleEvent(t, logs, allowlist.EnabledRole, TestNoRoleAddr, TestAdminAddr, allowlist.NoRole)
			},
		},
		{
			Name:       "admin_set_no_role",
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestEnabledAddr, allowlist.NoRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost + allowlist.AllowListEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				res := allowlist.GetAllowListStatus(state, contractAddress, TestEnabledAddr)
				require.Equal(t, allowlist.NoRole, res)
				// Check logs are stored in state
				logs := state.Logs()
				assertSetRoleEvent(t, logs, allowlist.NoRole, TestEnabledAddr, TestAdminAddr, allowlist.EnabledRole)
			},
		},
		{
			Name:       "no_role_set_no_role",
			Caller:     TestNoRoleAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestEnabledAddr, allowlist.NoRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList,
		},
		{
			Name:       "no_role_set_enabled",
			Caller:     TestNoRoleAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.EnabledRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList,
		},
		{
			Name:       "no_role_set_admin",
			Caller:     TestNoRoleAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestEnabledAddr, allowlist.AdminRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList,
		},
		{
			Name:       "enabled_set_no_role",
			Caller:     TestEnabledAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestAdminAddr, allowlist.NoRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList,
		},
		{
			Name:       "enabled_set_enabled",
			Caller:     TestEnabledAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.EnabledRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList,
		},
		{
			Name:       "enabled_set_admin",
			Caller:     TestEnabledAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.AdminRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList,
		},
		{
			Name:       "no_role_set_manager_pre_Durango",
			Caller:     TestNoRoleAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(false).AnyTimes()
				return config
			},
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.ManagerRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: 0,
			ReadOnly:    false,
			ExpectedErr: contract.ErrInvalidNonActivatedFunctionSelector,
		},
		{
			Name:       "no_role_set_manager",
			Caller:     TestNoRoleAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(true).AnyTimes()
				return config
			},
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.ManagerRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList,
		},
		{
			Name:       "enabled_role_set_manager_pre_Durango",
			Caller:     TestEnabledAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(false).AnyTimes()
				return config
			},
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.ManagerRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: 0,
			ReadOnly:    false,
			ExpectedErr: contract.ErrInvalidNonActivatedFunctionSelector,
		},
		{
			Name:       "enabled_set_manager",
			Caller:     TestNoRoleAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(true).AnyTimes()
				return config
			},
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.ManagerRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList,
		},
		{
			Name:       "admin_set_manager_pre_Durango",
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.ManagerRole)
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
			ExpectedErr: contract.ErrInvalidNonActivatedFunctionSelector,
		},
		{
			Name:       "admin_set_manager",
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.ManagerRole)
				require.NoError(t, err)

				return input
			},
			ExpectedRes: []byte{},
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(true).AnyTimes()
				return config
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost + allowlist.AllowListEventGasCost,
			ReadOnly:    false,
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				res := allowlist.GetAllowListStatus(state, contractAddress, TestNoRoleAddr)
				require.Equal(t, allowlist.ManagerRole, res)
				// Check logs are stored in state
				logs := state.Logs()
				assertSetRoleEvent(t, logs, allowlist.ManagerRole, TestNoRoleAddr, TestAdminAddr, allowlist.NoRole)
			},
		},
		{
			Name:       "manager_set_no_role_to_no_role",
			Caller:     TestManagerAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.NoRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost + allowlist.AllowListEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			ExpectedErr: nil,
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				res := allowlist.GetAllowListStatus(state, contractAddress, TestNoRoleAddr)
				require.Equal(t, allowlist.NoRole, res)
				// Check logs are stored in state
				logs := state.Logs()
				assertSetRoleEvent(t, logs, allowlist.NoRole, TestNoRoleAddr, TestManagerAddr, allowlist.NoRole)
			},
		},
		{
			Name:       "manager_set_no_role_to_enabled",
			Caller:     TestManagerAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.EnabledRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost + allowlist.AllowListEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			ExpectedErr: nil,
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				res := allowlist.GetAllowListStatus(state, contractAddress, TestNoRoleAddr)
				require.Equal(t, allowlist.EnabledRole, res)

				// Check logs are stored in state
				logs := state.Logs()
				assertSetRoleEvent(t, logs, allowlist.EnabledRole, TestNoRoleAddr, TestManagerAddr, allowlist.NoRole)
			},
		},
		{
			Name:       "manager_set_no_role_to_manager",
			Caller:     TestManagerAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(true).AnyTimes()
				return config
			},
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.ManagerRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList,
		},
		{
			Name:       "manager_set_no_role_to_admin",
			Caller:     TestManagerAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.AdminRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList,
		},
		{
			Name:       "manager_set_enabled_to_admin",
			Caller:     TestManagerAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestEnabledAddr, allowlist.AdminRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList,
		},
		{
			Name:       "manager_set_enabled_role_to_manager",
			Caller:     TestManagerAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(true).AnyTimes()
				return config
			},
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestEnabledAddr, allowlist.ManagerRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList,
		},
		{
			Name:       "manager_set_enabled_role_to_no_role",
			Caller:     TestManagerAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestEnabledAddr, allowlist.NoRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost + allowlist.AllowListEventGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				res := allowlist.GetAllowListStatus(state, contractAddress, TestNoRoleAddr)
				require.Equal(t, allowlist.NoRole, res)

				logs := state.Logs()
				assertSetRoleEvent(t, logs, allowlist.NoRole, TestEnabledAddr, TestManagerAddr, allowlist.EnabledRole)
			},
		},
		{
			Name:       "manager_set_admin_to_no_role",
			Caller:     TestManagerAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestAdminAddr, allowlist.NoRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList,
		},
		{
			Name:       "manager_set_admin_role_to_enabled",
			Caller:     TestManagerAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestAdminAddr, allowlist.EnabledRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList,
		},
		{
			Name:       "manager_set_admin_to_manager",
			Caller:     TestManagerAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(true).AnyTimes()
				return config
			},
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestAdminAddr, allowlist.ManagerRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList,
		},
		{
			Name:       "manager_set_manager_to_no_role",
			Caller:     TestManagerAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestManagerAddr, allowlist.NoRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList,
		},
		{
			Name:       "admin_set_no_role_with_readOnly_enabled",
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestEnabledAddr, allowlist.NoRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    true,
			ExpectedErr: vm.ErrWriteProtection,
		},
		{
			Name:       "admin_set_no_role_insufficient_gas",
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestEnabledAddr, allowlist.NoRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas,
		},
		{
			Name:       "no_role_read_allow_list",
			Caller:     TestNoRoleAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackReadAllowList(TestNoRoleAddr)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ReadAllowListGasCost,
			ReadOnly:    false,
			ExpectedRes: common.Hash(allowlist.NoRole).Bytes(),
		},
		{
			Name:       "admin_role_read_allow_list",
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackReadAllowList(TestAdminAddr)
				require.NoError(t, err)

				return input
			}, SuppliedGas: allowlist.ReadAllowListGasCost,
			ReadOnly:    false,
			ExpectedRes: common.Hash(allowlist.AdminRole).Bytes(),
		},
		{
			Name:       "admin_read_allow_list_with_readOnly_enabled",
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackReadAllowList(TestNoRoleAddr)
				require.NoError(t, err)

				return input
			}, SuppliedGas: allowlist.ReadAllowListGasCost,
			ReadOnly:    true,
			ExpectedRes: common.Hash(allowlist.NoRole).Bytes(),
		},
		{
			Name:       "admin_read_allow_list_out_of_gas",
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackReadAllowList(TestNoRoleAddr)
				require.NoError(t, err)

				return input
			}, SuppliedGas: allowlist.ReadAllowListGasCost - 1,
			ReadOnly:    true,
			ExpectedErr: vm.ErrOutOfGas,
		},
		{
			Name: "initial_config_sets_admins",
			Config: mkConfigWithAllowList(
				module,
				&allowlist.AllowListConfig{
					AdminAddresses: []common.Address{TestNoRoleAddr, TestEnabledAddr},
				},
			),
			SuppliedGas: 0,
			ReadOnly:    false,
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				require.Equal(t, allowlist.AdminRole, allowlist.GetAllowListStatus(state, contractAddress, TestNoRoleAddr))
				require.Equal(t, allowlist.AdminRole, allowlist.GetAllowListStatus(state, contractAddress, TestEnabledAddr))
			},
		},
		{
			Name: "initial_config_sets_managers",
			Config: mkConfigWithAllowList(
				module,
				&allowlist.AllowListConfig{
					ManagerAddresses: []common.Address{TestNoRoleAddr, TestEnabledAddr},
				},
			),
			SuppliedGas: 0,
			ReadOnly:    false,
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				require.Equal(t, allowlist.ManagerRole, allowlist.GetAllowListStatus(state, contractAddress, TestNoRoleAddr))
				require.Equal(t, allowlist.ManagerRole, allowlist.GetAllowListStatus(state, contractAddress, TestEnabledAddr))
			},
		},
		{
			Name: "initial_config_sets_enabled",
			Config: mkConfigWithAllowList(
				module,
				&allowlist.AllowListConfig{
					EnabledAddresses: []common.Address{TestNoRoleAddr, TestAdminAddr},
				},
			),
			SuppliedGas: 0,
			ReadOnly:    false,
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				require.Equal(t, allowlist.EnabledRole, allowlist.GetAllowListStatus(state, contractAddress, TestAdminAddr))
				require.Equal(t, allowlist.EnabledRole, allowlist.GetAllowListStatus(state, contractAddress, TestNoRoleAddr))
			},
		},
		{
			Name:       "admin_set_admin_pre_Durango",
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(false).AnyTimes()
				return config
			},
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.AdminRole)
				require.NoError(t, err)
				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, stateDB *extstate.StateDB) {
				logs := stateDB.Logs()
				require.Empty(t, logs)
			},
		},
		{
			Name:       "admin_set_enabled_pre_Durango",
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(false).AnyTimes()
				return config
			},
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.EnabledRole)
				require.NoError(t, err)
				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, stateDB *extstate.StateDB) {
				logs := stateDB.Logs()
				require.Empty(t, logs)
			},
		},
		{
			Name:       "admin_set_no_role_pre_Durango",
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			ChainConfigFn: func(ctrl *gomock.Controller) precompileconfig.ChainConfig {
				config := precompileconfig.NewMockChainConfig(ctrl)
				config.EXPECT().IsDurango(gomock.Any()).Return(false).AnyTimes()
				return config
			},
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestEnabledAddr, allowlist.NoRole)
				require.NoError(t, err)
				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, stateDB *extstate.StateDB) {
				logs := stateDB.Logs()
				require.Empty(t, logs)
			},
		},
	}
}

// SetDefaultRoles returns a BeforeHook that sets roles TestAdminAddr and TestEnabledAddr
// to have the AdminRole and EnabledRole respectively.
func SetDefaultRoles(contractAddress common.Address) func(t testing.TB, state *extstate.StateDB) {
	return func(t testing.TB, state *extstate.StateDB) {
		allowlist.SetAllowListRole(state, contractAddress, TestAdminAddr, allowlist.AdminRole)
		allowlist.SetAllowListRole(state, contractAddress, TestManagerAddr, allowlist.ManagerRole)
		allowlist.SetAllowListRole(state, contractAddress, TestEnabledAddr, allowlist.EnabledRole)
		require.Equal(t, allowlist.AdminRole, allowlist.GetAllowListStatus(state, contractAddress, TestAdminAddr))
		require.Equal(t, allowlist.ManagerRole, allowlist.GetAllowListStatus(state, contractAddress, TestManagerAddr))
		require.Equal(t, allowlist.EnabledRole, allowlist.GetAllowListStatus(state, contractAddress, TestEnabledAddr))
		require.Equal(t, allowlist.NoRole, allowlist.GetAllowListStatus(state, contractAddress, TestNoRoleAddr))
	}
}

func RunPrecompileWithAllowListTests(t *testing.T, module modules.Module, tests []precompiletest.PrecompileTest) {
	t.Helper()

	// Add the contract specific tests to the map of tests to run.
	precompiletest.RunPrecompileTests(
		t,
		module,
		append(tests, AllowListTests(t, module)...),
	)
}

func assertSetRoleEvent(t testing.TB, logs []*ethtypes.Log, role allowlist.Role, addr common.Address, caller common.Address, oldRole allowlist.Role) {
	require.Len(t, logs, 1)
	log := logs[0]
	require.Equal(
		t,
		[]common.Hash{
			allowlist.AllowListABI.Events["RoleSet"].ID,
			role.Hash(),
			common.BytesToHash(addr[:]),
			common.BytesToHash(caller[:]),
		},
		log.Topics,
	)
	require.Equal(t, oldRole.Bytes(), log.Data)
}
