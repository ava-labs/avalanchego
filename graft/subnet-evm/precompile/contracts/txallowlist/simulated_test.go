// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txallowlist_test

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi/bind"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/vmerrors"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist/allowlisttest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/txallowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/utilstest"
	"github.com/ava-labs/avalanchego/utils"

	sim "github.com/ava-labs/avalanchego/graft/subnet-evm/ethclient/simulated"
	allowlistbindings "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist/allowlisttest/bindings"
)

var (
	adminKey, _        = crypto.GenerateKey()
	unprivilegedKey, _ = crypto.GenerateKey()

	adminAddress        = crypto.PubkeyToAddress(adminKey.PublicKey)
	unprivilegedAddress = crypto.PubkeyToAddress(unprivilegedKey.PublicKey)
)

func TestMain(m *testing.M) {
	// Ensure libevm extras are registered for tests.
	core.RegisterExtras()
	customtypes.Register()
	params.RegisterExtras()
	m.Run()
}

func deployAllowListTest(t *testing.T, b *sim.Backend, auth *bind.TransactOpts) (common.Address, *allowlistbindings.AllowListTest) {
	t.Helper()
	addr, tx, contract, err := allowlistbindings.DeployAllowListTest(auth, b.Client(), txallowlist.ContractAddress)
	require.NoError(t, err)
	utilstest.WaitReceiptSuccessful(t, b, tx)
	return addr, contract
}

func TestTxAllowList(t *testing.T) {
	chainID := params.TestChainConfig.ChainID
	admin := utilstest.NewAuth(t, adminKey, chainID)
	unprivileged := utilstest.NewAuth(t, unprivilegedKey, chainID)

	type testCase struct {
		name string
		test func(t *testing.T, backend *sim.Backend, precompileIntf *allowlistbindings.IAllowList)
	}

	testCases := []testCase{
		{
			name: "should verify sender is admin",
			test: func(t *testing.T, _ *sim.Backend, allowList *allowlistbindings.IAllowList) {
				allowlisttest.VerifyRole(t, allowList, adminAddress, allowlist.AdminRole)
			},
		},
		{
			name: "should verify new address has no role",
			test: func(t *testing.T, backend *sim.Backend, allowList *allowlistbindings.IAllowList) {
				allowListTestAddr, _ := deployAllowListTest(t, backend, admin)
				allowlisttest.VerifyRole(t, allowList, allowListTestAddr, allowlist.NoRole)
			},
		},
		{
			name: "should not let non-enabled address submit txs",
			test: func(t *testing.T, backend *sim.Backend, allowList *allowlistbindings.IAllowList) {
				allowlisttest.VerifyRole(t, allowList, unprivilegedAddress, allowlist.NoRole)

				_, _, _, err := allowlistbindings.DeployAllowListTest(unprivileged, backend.Client(), txallowlist.ContractAddress)
				require.ErrorContains(t, err, vmerrors.ErrSenderAddressNotAllowListed.Error()) // //nolint:forbidigo // upstream error wrapped as string
			},
		},
		{
			name: "should verify contract correctly reports admin status",
			test: func(t *testing.T, backend *sim.Backend, allowList *allowlistbindings.IAllowList) {
				allowListTestAddr, allowListTest := deployAllowListTest(t, backend, admin)

				allowlisttest.VerifyRole(t, allowList, allowListTestAddr, allowlist.NoRole)

				isAdmin, err := allowListTest.IsAdmin(nil, allowListTestAddr)
				require.NoError(t, err)
				require.False(t, isAdmin)

				isAdmin, err = allowListTest.IsAdmin(nil, adminAddress)
				require.NoError(t, err)
				require.True(t, isAdmin)
			},
		},
		{
			name: "should allow admin to add contract as admin via precompile",
			test: func(t *testing.T, backend *sim.Backend, allowList *allowlistbindings.IAllowList) {
				allowListTestAddr, allowListTest := deployAllowListTest(t, backend, admin)

				allowlisttest.VerifyRole(t, allowList, allowListTestAddr, allowlist.NoRole)
				allowlisttest.SetAsAdmin(t, backend, allowList, admin, allowListTestAddr)
				allowlisttest.VerifyRole(t, allowList, allowListTestAddr, allowlist.AdminRole)

				isAdmin, err := allowListTest.IsAdmin(nil, allowListTestAddr)
				require.NoError(t, err)
				require.True(t, isAdmin)
			},
		},
		{
			name: "should allow admin to add enabled address via contract",
			test: func(t *testing.T, backend *sim.Backend, allowList *allowlistbindings.IAllowList) {
				allowListTestAddr, allowListTest := deployAllowListTest(t, backend, admin)
				otherContractAddr, _ := deployAllowListTest(t, backend, admin)

				allowlisttest.VerifyRole(t, allowList, allowListTestAddr, allowlist.NoRole)
				allowlisttest.SetAsAdmin(t, backend, allowList, admin, allowListTestAddr)
				allowlisttest.VerifyRole(t, allowList, allowListTestAddr, allowlist.AdminRole)

				tx, err := allowListTest.SetEnabled(admin, otherContractAddr)
				require.NoError(t, err)
				utilstest.WaitReceipt(t, backend, tx)

				isEnabled, err := allowListTest.IsEnabled(nil, otherContractAddr)
				require.NoError(t, err)
				require.True(t, isEnabled)
				allowlisttest.VerifyRole(t, allowList, otherContractAddr, allowlist.EnabledRole)
			},
		},
		{
			name: "should not allow enabled address to add another enabled",
			test: func(t *testing.T, backend *sim.Backend, allowList *allowlistbindings.IAllowList) {
				allowListTestAddr, allowListTest := deployAllowListTest(t, backend, admin)
				otherContractAddr, _ := deployAllowListTest(t, backend, admin)

				allowlisttest.SetAsEnabled(t, backend, allowList, admin, allowListTestAddr)
				allowlisttest.VerifyRole(t, allowList, allowListTestAddr, allowlist.EnabledRole)

				// Try to set another address as enabled - should fail
				_, err := allowListTest.SetEnabled(admin, otherContractAddr)
				require.ErrorContains(t, err, vm.ErrExecutionReverted.Error()) //nolint:forbidigo // upstream error wrapped as string

				allowlisttest.VerifyRole(t, allowList, otherContractAddr, allowlist.NoRole)
			},
		},
		{
			name: "should allow admin to revoke enabled",
			test: func(t *testing.T, backend *sim.Backend, allowList *allowlistbindings.IAllowList) {
				allowListTestAddr, allowListTest := deployAllowListTest(t, backend, admin)
				enabledContractAddr, _ := deployAllowListTest(t, backend, admin)

				allowlisttest.SetAsAdmin(t, backend, allowList, admin, allowListTestAddr)

				tx, err := allowListTest.SetEnabled(admin, enabledContractAddr)
				require.NoError(t, err)
				utilstest.WaitReceipt(t, backend, tx)

				isEnabled, err := allowListTest.IsEnabled(nil, enabledContractAddr)
				require.NoError(t, err)
				require.True(t, isEnabled)

				tx, err = allowListTest.Revoke(admin, enabledContractAddr)
				require.NoError(t, err)
				utilstest.WaitReceipt(t, backend, tx)

				allowlisttest.VerifyRole(t, allowList, enabledContractAddr, allowlist.NoRole)
			},
		},
		{
			name: "should allow manager to add enabled",
			test: func(t *testing.T, backend *sim.Backend, allowList *allowlistbindings.IAllowList) {
				managerContractAddr, managerContract := deployAllowListTest(t, backend, admin)
				enabledContractAddr, _ := deployAllowListTest(t, backend, admin)

				// Set contract as manager
				allowlisttest.SetAsManager(t, backend, allowList, admin, managerContractAddr)
				allowlisttest.VerifyRole(t, allowList, managerContractAddr, allowlist.ManagerRole)

				// Manager should be able to set enabled
				tx, err := managerContract.SetEnabled(admin, enabledContractAddr)
				require.NoError(t, err)
				utilstest.WaitReceipt(t, backend, tx)

				allowlisttest.VerifyRole(t, allowList, enabledContractAddr, allowlist.EnabledRole)
			},
		},
		{
			name: "should allow manager to revoke enabled",
			test: func(t *testing.T, backend *sim.Backend, allowList *allowlistbindings.IAllowList) {
				managerContractAddr, managerContract := deployAllowListTest(t, backend, admin)
				enabledContractAddr, _ := deployAllowListTest(t, backend, admin)

				allowlisttest.SetAsManager(t, backend, allowList, admin, managerContractAddr)

				allowlisttest.SetAsEnabled(t, backend, allowList, admin, enabledContractAddr)
				allowlisttest.VerifyRole(t, allowList, enabledContractAddr, allowlist.EnabledRole)

				tx, err := managerContract.Revoke(admin, enabledContractAddr)
				require.NoError(t, err)
				utilstest.WaitReceipt(t, backend, tx)

				allowlisttest.VerifyRole(t, allowList, enabledContractAddr, allowlist.NoRole)
			},
		},
		{
			name: "should not allow manager to revoke admin",
			test: func(t *testing.T, backend *sim.Backend, allowList *allowlistbindings.IAllowList) {
				managerContractAddr, managerContract := deployAllowListTest(t, backend, admin)
				adminContractAddr, _ := deployAllowListTest(t, backend, admin)

				allowlisttest.SetAsManager(t, backend, allowList, admin, managerContractAddr)
				allowlisttest.SetAsAdmin(t, backend, allowList, admin, adminContractAddr)

				_, err := managerContract.Revoke(admin, adminContractAddr)
				require.ErrorContains(t, err, vm.ErrExecutionReverted.Error()) //nolint:forbidigo // upstream error wrapped as string

				allowlisttest.VerifyRole(t, allowList, adminContractAddr, allowlist.AdminRole)
			},
		},
		{
			name: "should not allow manager to grant admin",
			test: func(t *testing.T, backend *sim.Backend, allowList *allowlistbindings.IAllowList) {
				managerContractAddr, managerContract := deployAllowListTest(t, backend, admin)
				otherContractAddr, _ := deployAllowListTest(t, backend, admin)

				allowlisttest.SetAsManager(t, backend, allowList, admin, managerContractAddr)

				_, err := managerContract.SetAdmin(admin, otherContractAddr)
				require.ErrorContains(t, err, vm.ErrExecutionReverted.Error()) //nolint:forbidigo // upstream error wrapped as string

				allowlisttest.VerifyRole(t, allowList, otherContractAddr, allowlist.NoRole)
			},
		},
		{
			name: "should not allow manager to grant manager",
			test: func(t *testing.T, backend *sim.Backend, allowList *allowlistbindings.IAllowList) {
				managerContractAddr, managerContract := deployAllowListTest(t, backend, admin)
				otherContractAddr, _ := deployAllowListTest(t, backend, admin)

				allowlisttest.SetAsManager(t, backend, allowList, admin, managerContractAddr)

				_, err := managerContract.SetManager(admin, otherContractAddr)
				require.ErrorContains(t, err, vm.ErrExecutionReverted.Error()) //nolint:forbidigo // upstream error wrapped as string

				allowlisttest.VerifyRole(t, allowList, otherContractAddr, allowlist.NoRole)
			},
		},
		{
			name: "should not allow manager to revoke manager",
			test: func(t *testing.T, backend *sim.Backend, allowList *allowlistbindings.IAllowList) {
				manager1Addr, manager1Contract := deployAllowListTest(t, backend, admin)
				manager2Addr, _ := deployAllowListTest(t, backend, admin)

				allowlisttest.SetAsManager(t, backend, allowList, admin, manager1Addr)
				allowlisttest.SetAsManager(t, backend, allowList, admin, manager2Addr)

				_, err := manager1Contract.Revoke(admin, manager2Addr)
				require.ErrorContains(t, err, vm.ErrExecutionReverted.Error()) //nolint:forbidigo // upstream error wrapped as string

				allowlisttest.VerifyRole(t, allowList, manager2Addr, allowlist.ManagerRole)
			},
		},
	}

	precompileCfg := txallowlist.NewConfig(utils.PointerTo[uint64](0), []common.Address{adminAddress}, nil, nil)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			backend := utilstest.NewBackendWithPrecompile(t, precompileCfg, []common.Address{adminAddress, unprivilegedAddress})
			defer backend.Close()

			allowList, err := allowlistbindings.NewIAllowList(txallowlist.ContractAddress, backend.Client())
			require.NoError(t, err)

			tc.test(t, backend, allowList)
		})
	}
}

func TestIAllowList_Events(t *testing.T) {
	precompileCfg := txallowlist.NewConfig(utils.PointerTo[uint64](0), []common.Address{adminAddress}, nil, nil)
	admin := utilstest.NewAuth(t, adminKey, params.TestChainConfig.ChainID)
	allowlisttest.RunAllowListEventTests(t, precompileCfg, txallowlist.ContractAddress, admin, adminAddress, unprivilegedAddress)
}
