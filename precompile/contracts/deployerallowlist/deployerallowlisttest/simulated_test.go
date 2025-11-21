// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// This test suite migrates the Hardhat tests for the ContractDeployerAllowList
// to Go using the simulated backend and the generated bindings in this package.
package deployerallowlisttest

import (
	"crypto/ecdsa"
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
	"github.com/ava-labs/subnet-evm/utils"

	sim "github.com/ava-labs/subnet-evm/ethclient/simulated"
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

func newAuth(t *testing.T, key *ecdsa.PrivateKey, chainID *big.Int) *bind.TransactOpts {
	t.Helper()
	auth, err := bind.NewKeyedTransactorWithChainID(key, chainID)
	require.NoError(t, err)
	return auth
}

func newBackendWithDeployerAllowList(t *testing.T) *sim.Backend {
	t.Helper()
	chainCfg := params.Copy(params.TestChainConfig)
	// Match the simulated backend chain ID used for signing (1337).
	chainCfg.ChainID = big.NewInt(1337)
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

func waitReceipt(t *testing.T, b *sim.Backend, tx *types.Transaction) *types.Receipt {
	t.Helper()
	b.Commit(true)
	receipt, err := b.Client().TransactionReceipt(t.Context(), tx.Hash())
	require.NoError(t, err, "failed to get transaction receipt")
	return receipt
}

// Helper functions to reduce test boilerplate

func deployAllowListTestContract(t *testing.T, b *sim.Backend, auth *bind.TransactOpts) (common.Address, *allowlisttest.AllowListTest) {
	t.Helper()
	addr, tx, contract, err := allowlisttest.DeployAllowListTest(auth, b.Client(), deployerallowlist.ContractAddress)
	require.NoError(t, err)
	receipt := waitReceipt(t, b, tx)
	require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status)
	return addr, contract
}

func verifyRole(t *testing.T, allowList *allowlisttest.IAllowList, address common.Address, expectedRole allowlist.Role) {
	t.Helper()
	role, err := allowList.ReadAllowList(nil, address)
	require.NoError(t, err)
	require.Equal(t, expectedRole.Big(), role)
}

func setAsAdmin(t *testing.T, b *sim.Backend, allowList *allowlisttest.IAllowList, auth *bind.TransactOpts, address common.Address) {
	t.Helper()
	tx, err := allowList.SetAdmin(auth, address)
	require.NoError(t, err)
	receipt := waitReceipt(t, b, tx)
	require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status)
}

func TestDeployerAllowList(t *testing.T) {
	chainID := big.NewInt(1337)
	admin := newAuth(t, adminKey, chainID)
	unprivileged := newAuth(t, unprivilegedKey, chainID)

	type testCase struct {
		name string
		test func(t *testing.T, backend *sim.Backend, precompileIntf *allowlisttest.IAllowList)
	}

	testCases := []testCase{
		{
			name: "should verify sender is admin",
			test: func(t *testing.T, _ *sim.Backend, allowList *allowlisttest.IAllowList) {
				verifyRole(t, allowList, adminAddress, allowlist.AdminRole)
			},
		},
		{
			name: "should verify new address has no role",
			test: func(t *testing.T, backend *sim.Backend, allowList *allowlisttest.IAllowList) {
				allowListTestAddr, _ := deployAllowListTestContract(t, backend, admin)
				verifyRole(t, allowList, allowListTestAddr, allowlist.NoRole)
			},
		},
		{
			name: "should verify contract correctly reports admin status",
			test: func(t *testing.T, backend *sim.Backend, allowList *allowlisttest.IAllowList) {
				allowListTestAddr, allowListTest := deployAllowListTestContract(t, backend, admin)

				verifyRole(t, allowList, allowListTestAddr, allowlist.NoRole)

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
			test: func(t *testing.T, backend *sim.Backend, allowList *allowlisttest.IAllowList) {
				verifyRole(t, allowList, unprivilegedAddress, allowlist.NoRole)

				_, allowListTest := deployAllowListTestContract(t, backend, admin)

				// Try to deploy via unprivileged user - should fail
				_, err := allowListTest.DeployContract(unprivileged)
				// The error returned is a JSON Error rather than the vm.ErrExecutionReverted error
				require.ErrorContains(t, err, vm.ErrExecutionReverted.Error())
			},
		},
		{
			name: "should allow admin to add contract as admin via precompile",
			test: func(t *testing.T, backend *sim.Backend, allowList *allowlisttest.IAllowList) {
				allowListTestAddr, allowListTest := deployAllowListTestContract(t, backend, admin)

				verifyRole(t, allowList, allowListTestAddr, allowlist.NoRole)
				setAsAdmin(t, backend, allowList, admin, allowListTestAddr)
				verifyRole(t, allowList, allowListTestAddr, allowlist.AdminRole)

				isAdmin, err := allowListTest.IsAdmin(nil, allowListTestAddr)
				require.NoError(t, err)
				require.True(t, isAdmin)
			},
		},
		{
			name: "should allow admin to add deployer via contract",
			test: func(t *testing.T, backend *sim.Backend, allowList *allowlisttest.IAllowList) {
				allowListTestAddr, allowListTest := deployAllowListTestContract(t, backend, admin)
				otherContractAddr, _ := deployAllowListTestContract(t, backend, admin)

				verifyRole(t, allowList, allowListTestAddr, allowlist.NoRole)
				setAsAdmin(t, backend, allowList, admin, allowListTestAddr)
				verifyRole(t, allowList, allowListTestAddr, allowlist.AdminRole)

				tx, err := allowListTest.SetEnabled(admin, otherContractAddr)
				require.NoError(t, err)
				waitReceipt(t, backend, tx)

				isEnabled, err := allowListTest.IsEnabled(nil, otherContractAddr)
				require.NoError(t, err)
				require.True(t, isEnabled)
				verifyRole(t, allowList, otherContractAddr, allowlist.EnabledRole)
			},
		},
		{
			name: "should allow enabled address to deploy contracts",
			test: func(t *testing.T, backend *sim.Backend, allowList *allowlisttest.IAllowList) {
				allowListTestAddr, allowListTest := deployAllowListTestContract(t, backend, admin)
				deployerContractAddr, deployerContract := deployAllowListTestContract(t, backend, admin)

				setAsAdmin(t, backend, allowList, admin, allowListTestAddr)

				tx, err := allowListTest.SetEnabled(admin, deployerContractAddr)
				require.NoError(t, err)
				waitReceipt(t, backend, tx)

				isEnabled, err := allowListTest.IsEnabled(nil, deployerContractAddr)
				require.NoError(t, err)
				require.True(t, isEnabled)

				tx, err = deployerContract.DeployContract(admin)
				require.NoError(t, err)
				receipt := waitReceipt(t, backend, tx)
				require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status)
			},
		},
		{
			name: "should allow admin to revoke deployer",
			test: func(t *testing.T, backend *sim.Backend, allowList *allowlisttest.IAllowList) {
				allowListTestAddr, allowListTest := deployAllowListTestContract(t, backend, admin)
				deployerContractAddr, _ := deployAllowListTestContract(t, backend, admin)

				setAsAdmin(t, backend, allowList, admin, allowListTestAddr)

				tx, err := allowListTest.SetEnabled(admin, deployerContractAddr)
				require.NoError(t, err)
				waitReceipt(t, backend, tx)

				isEnabled, err := allowListTest.IsEnabled(nil, deployerContractAddr)
				require.NoError(t, err)
				require.True(t, isEnabled)

				tx, err = allowListTest.Revoke(admin, deployerContractAddr)
				require.NoError(t, err)
				waitReceipt(t, backend, tx)

				verifyRole(t, allowList, deployerContractAddr, allowlist.NoRole)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			backend := newBackendWithDeployerAllowList(t)
			defer backend.Close()

			allowList, err := allowlisttest.NewIAllowList(deployerallowlist.ContractAddress, backend.Client())
			require.NoError(t, err)

			tc.test(t, backend, allowList)
		})
	}
}

func TestIAllowList_Events(t *testing.T) {
	chainID := big.NewInt(1337)
	admin := newAuth(t, adminKey, chainID)
	testKey, _ := crypto.GenerateKey()
	testAddress := crypto.PubkeyToAddress(testKey.PublicKey)

	type testCase struct {
		name           string
		testRun        func(*allowlisttest.IAllowList, *bind.TransactOpts, *sim.Backend, *testing.T, common.Address)
		expectedEvents []allowlisttest.IAllowListRoleSet
	}

	testCases := []testCase{
		{
			name: "should emit event after set admin",
			testRun: func(allowList *allowlisttest.IAllowList, auth *bind.TransactOpts, backend *sim.Backend, t *testing.T, addr common.Address) {
				tx, err := allowList.SetAdmin(auth, addr)
				require.NoError(t, err)
				waitReceipt(t, backend, tx)
			},
			expectedEvents: []allowlisttest.IAllowListRoleSet{
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
			testRun: func(allowList *allowlisttest.IAllowList, auth *bind.TransactOpts, backend *sim.Backend, t *testing.T, addr common.Address) {
				tx, err := allowList.SetManager(auth, addr)
				require.NoError(t, err)
				waitReceipt(t, backend, tx)
			},
			expectedEvents: []allowlisttest.IAllowListRoleSet{
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
			testRun: func(allowList *allowlisttest.IAllowList, auth *bind.TransactOpts, backend *sim.Backend, t *testing.T, addr common.Address) {
				tx, err := allowList.SetEnabled(auth, addr)
				require.NoError(t, err)
				waitReceipt(t, backend, tx)
			},
			expectedEvents: []allowlisttest.IAllowListRoleSet{
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
			testRun: func(allowList *allowlisttest.IAllowList, auth *bind.TransactOpts, backend *sim.Backend, t *testing.T, addr common.Address) {
				// First set the address to Enabled so we can test setting it to None
				tx, err := allowList.SetEnabled(auth, addr)
				require.NoError(t, err)
				waitReceipt(t, backend, tx)

				tx, err = allowList.SetNone(auth, addr)
				require.NoError(t, err)
				waitReceipt(t, backend, tx)
			},
			expectedEvents: []allowlisttest.IAllowListRoleSet{
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

			allowList, err := allowlisttest.NewIAllowList(deployerallowlist.ContractAddress, backend.Client())
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
