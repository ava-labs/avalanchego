// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package deployerallowlist_test

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/subnet-evm/accounts/abi/bind"
	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/params/extras"
	"github.com/ava-labs/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/subnet-evm/precompile/allowlist/allowlisttest"
	"github.com/ava-labs/subnet-evm/precompile/contracts/deployerallowlist"
	"github.com/ava-labs/subnet-evm/precompile/contracts/testutils"
	"github.com/ava-labs/subnet-evm/utils"

	sim "github.com/ava-labs/subnet-evm/ethclient/simulated"
	allowlistbindings "github.com/ava-labs/subnet-evm/precompile/allowlist/allowlisttest/bindings"
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

func newBackendWithDeployerAllowList(t *testing.T) *sim.Backend {
	t.Helper()
	chainCfg := params.Copy(params.TestChainConfig)
	// Enable ContractDeployerAllowList at genesis with admin set to adminAddress.
	params.GetExtra(&chainCfg).GenesisPrecompiles = extras.Precompiles{
		deployerallowlist.ConfigKey: deployerallowlist.NewConfig(utils.NewUint64(0), []common.Address{adminAddress}, nil, nil),
	}
	return sim.NewBackend(
		types.GenesisAlloc{
			adminAddress:        {Balance: big.NewInt(1000000000000000000)},
			unprivilegedAddress: {Balance: big.NewInt(1000000000000000000)},
		},
		sim.WithChainConfig(&chainCfg),
	)
}

// Helper functions to reduce test boilerplate

func deployAllowListTest(t *testing.T, b *sim.Backend, auth *bind.TransactOpts) (common.Address, *allowlistbindings.AllowListTest) {
	t.Helper()
	addr, tx, contract, err := allowlistbindings.DeployAllowListTest(auth, b.Client(), deployerallowlist.ContractAddress)
	require.NoError(t, err)
	testutils.WaitReceiptSuccessful(t, b, tx)
	return addr, contract
}

func TestDeployerAllowList(t *testing.T) {
	chainID := params.TestChainConfig.ChainID
	admin := testutils.NewAuth(t, adminKey, chainID)
	unprivileged := testutils.NewAuth(t, unprivilegedKey, chainID)

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
			name: "should not let address with no role deploy contracts",
			test: func(t *testing.T, backend *sim.Backend, allowList *allowlistbindings.IAllowList) {
				allowlisttest.VerifyRole(t, allowList, unprivilegedAddress, allowlist.NoRole)

				_, allowListTest := deployAllowListTest(t, backend, admin)

				// Try to deploy via unprivileged user - should fail
				_, err := allowListTest.DeployContract(unprivileged)
				// The error returned is a JSON Error rather than the vm.ErrExecutionReverted error
				require.ErrorContains(t, err, vm.ErrExecutionReverted.Error()) //nolint:forbidigo // uses upstream code
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
			name: "should allow admin to add deployer via contract",
			test: func(t *testing.T, backend *sim.Backend, allowList *allowlistbindings.IAllowList) {
				allowListTestAddr, allowListTest := deployAllowListTest(t, backend, admin)
				otherContractAddr, _ := deployAllowListTest(t, backend, admin)

				allowlisttest.VerifyRole(t, allowList, allowListTestAddr, allowlist.NoRole)
				allowlisttest.SetAsAdmin(t, backend, allowList, admin, allowListTestAddr)
				allowlisttest.VerifyRole(t, allowList, allowListTestAddr, allowlist.AdminRole)

				tx, err := allowListTest.SetEnabled(admin, otherContractAddr)
				require.NoError(t, err)
				testutils.WaitReceipt(t, backend, tx)

				isEnabled, err := allowListTest.IsEnabled(nil, otherContractAddr)
				require.NoError(t, err)
				require.True(t, isEnabled)
				allowlisttest.VerifyRole(t, allowList, otherContractAddr, allowlist.EnabledRole)
			},
		},
		{
			name: "should allow enabled address to deploy contracts",
			test: func(t *testing.T, backend *sim.Backend, allowList *allowlistbindings.IAllowList) {
				allowListTestAddr, allowListTest := deployAllowListTest(t, backend, admin)
				deployerContractAddr, deployerContract := deployAllowListTest(t, backend, admin)

				allowlisttest.SetAsAdmin(t, backend, allowList, admin, allowListTestAddr)

				tx, err := allowListTest.SetEnabled(admin, deployerContractAddr)
				require.NoError(t, err)
				testutils.WaitReceipt(t, backend, tx)

				isEnabled, err := allowListTest.IsEnabled(nil, deployerContractAddr)
				require.NoError(t, err)
				require.True(t, isEnabled)

				tx, err = deployerContract.DeployContract(admin)
				require.NoError(t, err)
				testutils.WaitReceiptSuccessful(t, backend, tx)
			},
		},
		{
			name: "should allow admin to revoke deployer",
			test: func(t *testing.T, backend *sim.Backend, allowList *allowlistbindings.IAllowList) {
				allowListTestAddr, allowListTest := deployAllowListTest(t, backend, admin)
				deployerContractAddr, _ := deployAllowListTest(t, backend, admin)

				allowlisttest.SetAsAdmin(t, backend, allowList, admin, allowListTestAddr)

				tx, err := allowListTest.SetEnabled(admin, deployerContractAddr)
				require.NoError(t, err)
				testutils.WaitReceipt(t, backend, tx)

				isEnabled, err := allowListTest.IsEnabled(nil, deployerContractAddr)
				require.NoError(t, err)
				require.True(t, isEnabled)

				tx, err = allowListTest.Revoke(admin, deployerContractAddr)
				require.NoError(t, err)
				testutils.WaitReceipt(t, backend, tx)

				allowlisttest.VerifyRole(t, allowList, deployerContractAddr, allowlist.NoRole)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			backend := newBackendWithDeployerAllowList(t)
			defer backend.Close()

			allowList, err := allowlistbindings.NewIAllowList(deployerallowlist.ContractAddress, backend.Client())
			require.NoError(t, err)

			tc.test(t, backend, allowList)
		})
	}
}

func TestIAllowList_Events(t *testing.T) {
	chainID := params.TestChainConfig.ChainID
	admin := testutils.NewAuth(t, adminKey, chainID)
	testKey, _ := crypto.GenerateKey()
	testAddress := crypto.PubkeyToAddress(testKey.PublicKey)

	type testCase struct {
		name           string
		testRun        func(*allowlistbindings.IAllowList, *bind.TransactOpts, *sim.Backend, *testing.T, common.Address)
		expectedEvents []allowlistbindings.IAllowListRoleSet
	}

	testCases := []testCase{
		{
			name: "should emit event after set admin",
			testRun: func(allowList *allowlistbindings.IAllowList, auth *bind.TransactOpts, backend *sim.Backend, t *testing.T, addr common.Address) {
				tx, err := allowList.SetAdmin(auth, addr)
				require.NoError(t, err)
				testutils.WaitReceipt(t, backend, tx)
			},
			expectedEvents: []allowlistbindings.IAllowListRoleSet{
				{
					Role:    allowlist.AdminRole.Big(),
					Account: testAddress,
					Sender:  adminAddress,
					OldRole: allowlist.NoRole.Big(),
				},
			},
		},
		{
			name: "should emit event after set manager",
			testRun: func(allowList *allowlistbindings.IAllowList, auth *bind.TransactOpts, backend *sim.Backend, t *testing.T, addr common.Address) {
				tx, err := allowList.SetManager(auth, addr)
				require.NoError(t, err)
				testutils.WaitReceipt(t, backend, tx)
			},
			expectedEvents: []allowlistbindings.IAllowListRoleSet{
				{
					Role:    allowlist.ManagerRole.Big(),
					Account: testAddress,
					Sender:  adminAddress,
					OldRole: allowlist.NoRole.Big(),
				},
			},
		},
		{
			name: "should emit event after set enabled",
			testRun: func(allowList *allowlistbindings.IAllowList, auth *bind.TransactOpts, backend *sim.Backend, t *testing.T, addr common.Address) {
				tx, err := allowList.SetEnabled(auth, addr)
				require.NoError(t, err)
				testutils.WaitReceipt(t, backend, tx)
			},
			expectedEvents: []allowlistbindings.IAllowListRoleSet{
				{
					Role:    allowlist.EnabledRole.Big(),
					Account: testAddress,
					Sender:  adminAddress,
					OldRole: allowlist.NoRole.Big(),
				},
			},
		},
		{
			name: "should emit event after set none",
			testRun: func(allowList *allowlistbindings.IAllowList, auth *bind.TransactOpts, backend *sim.Backend, t *testing.T, addr common.Address) {
				// First set the address to Enabled so we can test setting it to None
				tx, err := allowList.SetEnabled(auth, addr)
				require.NoError(t, err)
				testutils.WaitReceipt(t, backend, tx)

				tx, err = allowList.SetNone(auth, addr)
				require.NoError(t, err)
				testutils.WaitReceipt(t, backend, tx)
			},
			expectedEvents: []allowlistbindings.IAllowListRoleSet{
				{
					Role:    allowlist.EnabledRole.Big(),
					Account: testAddress,
					Sender:  adminAddress,
					OldRole: allowlist.NoRole.Big(),
				},
				{
					Role:    allowlist.NoRole.Big(),
					Account: testAddress,
					Sender:  adminAddress,
					OldRole: allowlist.EnabledRole.Big(),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require := require.New(t)

			backend := newBackendWithDeployerAllowList(t)
			defer backend.Close()

			allowList, err := allowlistbindings.NewIAllowList(deployerallowlist.ContractAddress, backend.Client())
			require.NoError(err)

			tc.testRun(allowList, admin, backend, t, testAddress)

			// Filter for RoleSet events using FilterRoleSet
			// This will filter for all RoleSet events.
			iter, err := allowList.FilterRoleSet(
				nil,
				nil,
				nil,
				nil,
			)
			require.NoError(err)
			defer iter.Close()

			// Verify event fields match expected values
			for _, expectedEvent := range tc.expectedEvents {
				require.True(iter.Next(), "expected to find RoleSet event")
				event := iter.Event
				require.Zero(expectedEvent.Role.Cmp(event.Role), "role mismatch")
				require.Equal(expectedEvent.Account, event.Account, "account mismatch")
				require.Equal(expectedEvent.Sender, event.Sender, "sender mismatch")
				require.Zero(expectedEvent.OldRole.Cmp(event.OldRole), "oldRole mismatch")
			}

			// Verify there are no more events
			require.False(iter.Next(), "expected no more RoleSet events")
			require.NoError(iter.Error())
		})
	}
}
