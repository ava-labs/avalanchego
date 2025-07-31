// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package allowlisttest

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	ethtypes "github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/subnet-evm/core/extstate"
	"github.com/ava-labs/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/subnet-evm/precompile/modules"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/subnet-evm/precompile/precompiletest"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

var (
	TestAdminAddr   = common.HexToAddress("0x0000000000000000000000000000000000000011")
	TestEnabledAddr = common.HexToAddress("0x0000000000000000000000000000000000000022")
	TestNoRoleAddr  = common.HexToAddress("0x0000000000000000000000000000000000000033")
	TestManagerAddr = common.HexToAddress("0x0000000000000000000000000000000000000044")
)

func AllowListTests(t testing.TB, module modules.Module) map[string]precompiletest.PrecompileTest {
	contractAddress := module.Address
	return map[string]precompiletest.PrecompileTest{
		"admin set admin": {
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
		"admin set enabled": {
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
		"admin set no role": {
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
		"no role set no role": {
			Caller:     TestNoRoleAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestEnabledAddr, allowlist.NoRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList.Error(),
		},
		"no role set enabled": {
			Caller:     TestNoRoleAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.EnabledRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList.Error(),
		},
		"no role set admin": {
			Caller:     TestNoRoleAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestEnabledAddr, allowlist.AdminRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList.Error(),
		},
		"enabled set no role": {
			Caller:     TestEnabledAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestAdminAddr, allowlist.NoRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList.Error(),
		},
		"enabled set enabled": {
			Caller:     TestEnabledAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.EnabledRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList.Error(),
		},
		"enabled set admin": {
			Caller:     TestEnabledAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.AdminRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList.Error(),
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
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.ManagerRole)
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
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.ManagerRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList.Error(),
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
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.ManagerRole)
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
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.ManagerRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList.Error(),
		},
		"admin set manager pre-DUpgarde": {
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
			ExpectedErr: "invalid non-activated function selector",
		},
		"admin set manager": {
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
		"manager set no role to no role": {
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
			ExpectedErr: "",
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				res := allowlist.GetAllowListStatus(state, contractAddress, TestNoRoleAddr)
				require.Equal(t, allowlist.NoRole, res)
				// Check logs are stored in state
				logs := state.Logs()
				assertSetRoleEvent(t, logs, allowlist.NoRole, TestNoRoleAddr, TestManagerAddr, allowlist.NoRole)
			},
		},
		"manager set no role to enabled": {
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
			ExpectedErr: "",
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				res := allowlist.GetAllowListStatus(state, contractAddress, TestNoRoleAddr)
				require.Equal(t, allowlist.EnabledRole, res)

				// Check logs are stored in state
				logs := state.Logs()
				assertSetRoleEvent(t, logs, allowlist.EnabledRole, TestNoRoleAddr, TestManagerAddr, allowlist.NoRole)
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
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.ManagerRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList.Error(),
		},
		"manager set no role to admin": {
			Caller:     TestManagerAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.AdminRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList.Error(),
		},
		"manager set enabled to admin": {
			Caller:     TestManagerAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestEnabledAddr, allowlist.AdminRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList.Error(),
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
				input, err := allowlist.PackModifyAllowList(TestEnabledAddr, allowlist.ManagerRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList.Error(),
		},
		"manager set enabled role to no role": {
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

				// Check logs are stored in state
				logs := state.Logs()
				assertSetRoleEvent(t, logs, allowlist.NoRole, TestEnabledAddr, TestManagerAddr, allowlist.EnabledRole)
			},
		},
		"manager set admin to no role": {
			Caller:     TestManagerAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestAdminAddr, allowlist.NoRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList.Error(),
		},
		"manager set admin role to enabled": {
			Caller:     TestManagerAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestAdminAddr, allowlist.EnabledRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList.Error(),
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
				input, err := allowlist.PackModifyAllowList(TestAdminAddr, allowlist.ManagerRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList.Error(),
		},
		"manager set manager to no role": {
			Caller:     TestManagerAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestManagerAddr, allowlist.NoRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedErr: allowlist.ErrCannotModifyAllowList.Error(),
		},
		"admin set no role with readOnly enabled": {
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestEnabledAddr, allowlist.NoRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    true,
			ExpectedErr: vm.ErrWriteProtection.Error(),
		},
		"admin set no role insufficient gas": {
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackModifyAllowList(TestEnabledAddr, allowlist.NoRole)
				require.NoError(t, err)

				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
		"no role read allow list": {
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
		"admin role read allow list": {
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
		"admin read allow list with readOnly enabled": {
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
		"radmin read allow list out of gas": {
			Caller:     TestAdminAddr,
			BeforeHook: SetDefaultRoles(contractAddress),
			InputFn: func(t testing.TB) []byte {
				input, err := allowlist.PackReadAllowList(TestNoRoleAddr)
				require.NoError(t, err)

				return input
			}, SuppliedGas: allowlist.ReadAllowListGasCost - 1,
			ReadOnly:    true,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
		"initial config sets admins": {
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
		"initial config sets managers": {
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
		"initial config sets enabled": {
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
		"admin set admin pre-Durango": {
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
				// Check no logs are stored in state
				logs := stateDB.Logs()
				require.Empty(t, logs)
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
				input, err := allowlist.PackModifyAllowList(TestNoRoleAddr, allowlist.EnabledRole)
				require.NoError(t, err)
				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, stateDB *extstate.StateDB) {
				// Check no logs are stored in state
				logs := stateDB.Logs()
				require.Empty(t, logs)
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
				input, err := allowlist.PackModifyAllowList(TestEnabledAddr, allowlist.NoRole)
				require.NoError(t, err)
				return input
			},
			SuppliedGas: allowlist.ModifyAllowListGasCost,
			ReadOnly:    false,
			ExpectedRes: []byte{},
			AfterHook: func(t testing.TB, stateDB *extstate.StateDB) {
				// Check no logs are stored in state
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

func RunPrecompileWithAllowListTests(t *testing.T, module modules.Module, contractTests map[string]precompiletest.PrecompileTest) {
	t.Helper()
	tests := AllowListTests(t, module)
	// Add the contract specific tests to the map of tests to run.
	for name, test := range contractTests {
		if _, exists := tests[name]; exists {
			t.Fatalf("duplicate test name: %s", name)
		}
		tests[name] = test
	}

	precompiletest.RunPrecompileTests(t, module, tests)
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
