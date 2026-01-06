// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package deployerallowlist_test

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
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist/allowlisttest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/deployerallowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/utilstest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/utils"

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
	addr, tx, contract, err := allowlistbindings.DeployAllowListTest(auth, b.Client(), deployerallowlist.ContractAddress)
	require.NoError(t, err)
	utilstest.WaitReceiptSuccessful(t, b, tx)
	return addr, contract
}

func TestDeployerAllowList(t *testing.T) {
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
				utilstest.WaitReceipt(t, backend, tx)

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
				utilstest.WaitReceipt(t, backend, tx)

				isEnabled, err := allowListTest.IsEnabled(nil, deployerContractAddr)
				require.NoError(t, err)
				require.True(t, isEnabled)

				tx, err = deployerContract.DeployContract(admin)
				require.NoError(t, err)
				utilstest.WaitReceiptSuccessful(t, backend, tx)
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
				utilstest.WaitReceipt(t, backend, tx)

				isEnabled, err := allowListTest.IsEnabled(nil, deployerContractAddr)
				require.NoError(t, err)
				require.True(t, isEnabled)

				tx, err = allowListTest.Revoke(admin, deployerContractAddr)
				require.NoError(t, err)
				utilstest.WaitReceipt(t, backend, tx)

				allowlisttest.VerifyRole(t, allowList, deployerContractAddr, allowlist.NoRole)
			},
		},
	}

	precompileCfg := deployerallowlist.NewConfig(utils.NewUint64(0), []common.Address{adminAddress}, nil, nil)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			backend := utilstest.NewBackendWithPrecompile(t, precompileCfg, []common.Address{adminAddress, unprivilegedAddress})
			defer backend.Close()

			allowList, err := allowlistbindings.NewIAllowList(deployerallowlist.ContractAddress, backend.Client())
			require.NoError(t, err)

			tc.test(t, backend, allowList)
		})
	}
}

func TestIAllowList_Events(t *testing.T) {
	precompileCfg := deployerallowlist.NewConfig(utils.NewUint64(0), []common.Address{adminAddress}, nil, nil)
	admin := utilstest.NewAuth(t, adminKey, params.TestChainConfig.ChainID)
	allowlisttest.RunAllowListEventTests(t, precompileCfg, deployerallowlist.ContractAddress, admin, adminAddress, unprivilegedAddress)
}
