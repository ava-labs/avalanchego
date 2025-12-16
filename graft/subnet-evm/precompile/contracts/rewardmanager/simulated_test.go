// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rewardmanager_test

import (
	"crypto/ecdsa"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/constants"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi/bind"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/eth/ethconfig"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/node"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist/allowlisttest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/rewardmanager"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/utilstest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/utils"

	sim "github.com/ava-labs/avalanchego/graft/subnet-evm/ethclient/simulated"
	rewardmanagerbindings "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/rewardmanager/rewardmanagertest/bindings"
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

func deployRewardManagerTest(t *testing.T, b *sim.Backend, auth *bind.TransactOpts) (common.Address, *rewardmanagerbindings.RewardManagerTest) {
	t.Helper()
	addr, tx, contract, err := rewardmanagerbindings.DeployRewardManagerTest(auth, b.Client(), rewardmanager.ContractAddress)
	require.NoError(t, err)
	utilstest.WaitReceiptSuccessful(t, b, tx)
	return addr, contract
}

// sendSimpleTx sends a simple ETH transfer transaction
// See ethclient/simulated/backend_test.go newTx() for the source of this code
// TODO(jonathanoppenheimer): after libevmifiying the geth code, investigate whether we can use the same code for both
func sendSimpleTx(t *testing.T, b *sim.Backend, key *ecdsa.PrivateKey) *types.Transaction {
	t.Helper()
	client := b.Client()
	addr := crypto.PubkeyToAddress(key.PublicKey)

	chainID, err := client.ChainID(t.Context())
	require.NoError(t, err)

	nonce, err := client.NonceAt(t.Context(), addr, nil)
	require.NoError(t, err)

	head, err := client.HeaderByNumber(t.Context(), nil)
	require.NoError(t, err)

	gasPrice := new(big.Int).Add(head.BaseFee, big.NewInt(params.GWei))

	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: big.NewInt(params.GWei),
		GasFeeCap: gasPrice,
		Gas:       21000,
		To:        &addr,
		Value:     big.NewInt(0),
	})

	signedTx, err := types.SignTx(tx, types.LatestSignerForChainID(chainID), key)
	require.NoError(t, err)

	err = client.SendTransaction(t.Context(), signedTx)
	require.NoError(t, err)

	return signedTx
}

func TestRewardManager(t *testing.T) {
	chainID := params.TestChainConfig.ChainID
	admin := utilstest.NewAuth(t, adminKey, chainID)

	type testCase struct {
		name                string
		initialRewardConfig *rewardmanager.InitialRewardConfig      // optional
		backendOpts         []func(*node.Config, *ethconfig.Config) // optional
		test                func(t *testing.T, backend *sim.Backend, rewardManagerIntf *rewardmanagerbindings.IRewardManager)
	}

	testCases := []testCase{
		{
			name: "should verify sender is admin",
			test: func(t *testing.T, _ *sim.Backend, rewardManager *rewardmanagerbindings.IRewardManager) {
				allowlisttest.VerifyRole(t, rewardManager, adminAddress, allowlist.AdminRole)
			},
		},
		{
			name: "should verify new address has no role",
			test: func(t *testing.T, backend *sim.Backend, rewardManager *rewardmanagerbindings.IRewardManager) {
				testContractAddr, _ := deployRewardManagerTest(t, backend, admin)
				allowlisttest.VerifyRole(t, rewardManager, testContractAddr, allowlist.NoRole)
			},
		},
		{
			name: "should not allow non-enabled to set reward address",
			test: func(t *testing.T, backend *sim.Backend, rewardManager *rewardmanagerbindings.IRewardManager) {
				testContractAddr, testContract := deployRewardManagerTest(t, backend, admin)

				allowlisttest.VerifyRole(t, rewardManager, testContractAddr, allowlist.NoRole)

				_, err := testContract.SetRewardAddress(admin, testContractAddr)
				require.ErrorContains(t, err, vm.ErrExecutionReverted.Error()) //nolint:forbidigo // upstream error wrapped as string
			},
		},
		{
			name: "should allow admin to enable contract",
			test: func(t *testing.T, backend *sim.Backend, rewardManager *rewardmanagerbindings.IRewardManager) {
				testContractAddr, _ := deployRewardManagerTest(t, backend, admin)

				allowlisttest.VerifyRole(t, rewardManager, testContractAddr, allowlist.NoRole)
				allowlisttest.SetAsEnabled(t, backend, rewardManager, admin, testContractAddr)
				allowlisttest.VerifyRole(t, rewardManager, testContractAddr, allowlist.EnabledRole)
			},
		},
		{
			name: "should allow enabled contract to set reward address",
			test: func(t *testing.T, backend *sim.Backend, rewardManager *rewardmanagerbindings.IRewardManager) {
				testContractAddr, testContract := deployRewardManagerTest(t, backend, admin)

				allowlisttest.SetAsEnabled(t, backend, rewardManager, admin, testContractAddr)

				tx, err := testContract.SetRewardAddress(admin, testContractAddr)
				require.NoError(t, err)
				utilstest.WaitReceiptSuccessful(t, backend, tx)

				currentAddr, err := testContract.CurrentRewardAddress(nil)
				require.NoError(t, err)
				require.Equal(t, testContractAddr, currentAddr)
			},
		},
		{
			name: "should return false for areFeeRecipientsAllowed by default",
			test: func(t *testing.T, backend *sim.Backend, rewardManager *rewardmanagerbindings.IRewardManager) {
				testContractAddr, testContract := deployRewardManagerTest(t, backend, admin)
				allowlisttest.SetAsEnabled(t, backend, rewardManager, admin, testContractAddr)

				isAllowed, err := testContract.AreFeeRecipientsAllowed(nil)
				require.NoError(t, err)
				require.False(t, isAllowed)
			},
		},
		{
			name: "should allow enabled contract to allow fee recipients",
			test: func(t *testing.T, backend *sim.Backend, rewardManager *rewardmanagerbindings.IRewardManager) {
				testContractAddr, testContract := deployRewardManagerTest(t, backend, admin)
				allowlisttest.SetAsEnabled(t, backend, rewardManager, admin, testContractAddr)

				tx, err := testContract.AllowFeeRecipients(admin)
				require.NoError(t, err)
				utilstest.WaitReceiptSuccessful(t, backend, tx)

				isAllowed, err := testContract.AreFeeRecipientsAllowed(nil)
				require.NoError(t, err)
				require.True(t, isAllowed)
			},
		},
		{
			name: "should allow enabled contract to disable rewards",
			test: func(t *testing.T, backend *sim.Backend, rewardManager *rewardmanagerbindings.IRewardManager) {
				testContractAddr, testContract := deployRewardManagerTest(t, backend, admin)
				allowlisttest.SetAsEnabled(t, backend, rewardManager, admin, testContractAddr)

				tx, err := testContract.SetRewardAddress(admin, testContractAddr)
				require.NoError(t, err)
				utilstest.WaitReceiptSuccessful(t, backend, tx)

				currentAddr, err := testContract.CurrentRewardAddress(nil)
				require.NoError(t, err)
				require.Equal(t, testContractAddr, currentAddr)

				tx, err = testContract.DisableRewards(admin)
				require.NoError(t, err)
				utilstest.WaitReceiptSuccessful(t, backend, tx)

				currentAddr, err = testContract.CurrentRewardAddress(nil)
				require.NoError(t, err)
				require.Equal(t, constants.BlackholeAddr, currentAddr)
			},
		},
		{
			name: "should return blackhole as default reward address",
			test: func(t *testing.T, _ *sim.Backend, rewardManager *rewardmanagerbindings.IRewardManager) {
				currentAddr, err := rewardManager.CurrentRewardAddress(nil)
				require.NoError(t, err)
				require.Equal(t, constants.BlackholeAddr, currentAddr)
			},
		},
		{
			name: "fees should go to blackhole by default",
			test: func(t *testing.T, backend *sim.Backend, _ *rewardmanagerbindings.IRewardManager) {
				client := backend.Client()

				initialBlackholeBalance, err := client.BalanceAt(t.Context(), constants.BlackholeAddr, nil)
				require.NoError(t, err)

				tx := sendSimpleTx(t, backend, adminKey)
				utilstest.WaitReceiptSuccessful(t, backend, tx)

				newBlackholeBalance, err := client.BalanceAt(t.Context(), constants.BlackholeAddr, nil)
				require.NoError(t, err)

				require.Positive(t, newBlackholeBalance.Cmp(initialBlackholeBalance),
					"blackhole balance should have increased from fees")
			},
		},
		{
			name: "fees should go to configured reward address",
			test: func(t *testing.T, backend *sim.Backend, rewardManager *rewardmanagerbindings.IRewardManager) {
				client := backend.Client()

				testContractAddr, testContract := deployRewardManagerTest(t, backend, admin)
				allowlisttest.SetAsEnabled(t, backend, rewardManager, admin, testContractAddr)

				rewardRecipientKey, _ := crypto.GenerateKey()
				rewardRecipientAddr := crypto.PubkeyToAddress(rewardRecipientKey.PublicKey)

				initialRecipientBalance, err := client.BalanceAt(t.Context(), rewardRecipientAddr, nil)
				require.NoError(t, err)

				tx, err := testContract.SetRewardAddress(admin, rewardRecipientAddr)
				require.NoError(t, err)
				utilstest.WaitReceiptSuccessful(t, backend, tx)

				currentAddr, err := testContract.CurrentRewardAddress(nil)
				require.NoError(t, err)
				require.Equal(t, rewardRecipientAddr, currentAddr)

				// The fees from this transaction should go to the reward address
				tx = sendSimpleTx(t, backend, adminKey)
				utilstest.WaitReceiptSuccessful(t, backend, tx)

				newRecipientBalance, err := client.BalanceAt(t.Context(), rewardRecipientAddr, nil)
				require.NoError(t, err)

				require.Positive(t, newRecipientBalance.Cmp(initialRecipientBalance),
					"reward recipient balance should have increased from fees")
			},
		},
		{
			name:                "fees should go to coinbase when allowFeeRecipients is enabled",
			initialRewardConfig: &rewardmanager.InitialRewardConfig{AllowFeeRecipients: true},
			backendOpts: []func(*node.Config, *ethconfig.Config){
				sim.WithEtherbase(crypto.PubkeyToAddress(unprivilegedKey.PublicKey)), // use unprivilegedAddress as coinbase
			},
			test: func(t *testing.T, backend *sim.Backend, _ *rewardmanagerbindings.IRewardManager) {
				client := backend.Client()
				coinbaseAddr := unprivilegedAddress

				_, testContract := deployRewardManagerTest(t, backend, admin)

				isAllowed, err := testContract.AreFeeRecipientsAllowed(nil)
				require.NoError(t, err)
				require.True(t, isAllowed, "fee recipients should be allowed")

				initialCoinbaseBalance, err := client.BalanceAt(t.Context(), coinbaseAddr, nil)
				require.NoError(t, err)

				// The fees from this transaction should go to the coinbase address
				tx := sendSimpleTx(t, backend, adminKey)
				utilstest.WaitReceiptSuccessful(t, backend, tx)

				newCoinbaseBalance, err := client.BalanceAt(t.Context(), coinbaseAddr, nil)
				require.NoError(t, err)

				require.Positive(t, newCoinbaseBalance.Cmp(initialCoinbaseBalance),
					"coinbase balance should have increased from fees")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := rewardmanager.NewConfig(utils.NewUint64(0), []common.Address{adminAddress}, nil, nil, tc.initialRewardConfig)
			backend := utilstest.NewBackendWithPrecompile(t, cfg, []common.Address{adminAddress, unprivilegedAddress}, tc.backendOpts...)
			defer backend.Close()

			rewardManager, err := rewardmanagerbindings.NewIRewardManager(rewardmanager.ContractAddress, backend.Client())
			require.NoError(t, err)

			tc.test(t, backend, rewardManager)
		})
	}
}

func TestIRewardManager_Events(t *testing.T) {
	chainID := params.TestChainConfig.ChainID
	admin := utilstest.NewAuth(t, adminKey, chainID)

	type testCase struct {
		name string
		test func(t *testing.T, backend *sim.Backend, rewardManager *rewardmanagerbindings.IRewardManager)
	}

	testCases := []testCase{
		{
			name: "should emit RewardAddressChanged event",
			test: func(t *testing.T, backend *sim.Backend, rewardManager *rewardmanagerbindings.IRewardManager) {
				testContractAddr, testContract := deployRewardManagerTest(t, backend, admin)
				allowlisttest.SetAsEnabled(t, backend, rewardManager, admin, testContractAddr)

				rewardRecipientKey, _ := crypto.GenerateKey()
				rewardRecipientAddr := crypto.PubkeyToAddress(rewardRecipientKey.PublicKey)

				tx, err := testContract.SetRewardAddress(admin, rewardRecipientAddr)
				require.NoError(t, err)
				utilstest.WaitReceiptSuccessful(t, backend, tx)

				iter, err := rewardManager.FilterRewardAddressChanged(nil, nil, nil, nil)
				require.NoError(t, err)
				defer iter.Close()

				require.True(t, iter.Next(), "expected to find RewardAddressChanged event")
				event := iter.Event
				require.Equal(t, testContractAddr, event.Sender)
				require.Equal(t, constants.BlackholeAddr, event.OldRewardAddress)
				require.Equal(t, rewardRecipientAddr, event.NewRewardAddress)

				require.False(t, iter.Next(), "expected no more events")
				require.NoError(t, iter.Error())
			},
		},
		{
			name: "should emit FeeRecipientsAllowed event",
			test: func(t *testing.T, backend *sim.Backend, rewardManager *rewardmanagerbindings.IRewardManager) {
				testContractAddr, testContract := deployRewardManagerTest(t, backend, admin)
				allowlisttest.SetAsEnabled(t, backend, rewardManager, admin, testContractAddr)

				tx, err := testContract.AllowFeeRecipients(admin)
				require.NoError(t, err)
				utilstest.WaitReceiptSuccessful(t, backend, tx)

				iter, err := rewardManager.FilterFeeRecipientsAllowed(nil, nil)
				require.NoError(t, err)
				defer iter.Close()

				require.True(t, iter.Next(), "expected to find FeeRecipientsAllowed event")
				require.Equal(t, testContractAddr, iter.Event.Sender)

				require.False(t, iter.Next(), "expected no more events")
				require.NoError(t, iter.Error())
			},
		},
		{
			name: "should emit RewardsDisabled event",
			test: func(t *testing.T, backend *sim.Backend, rewardManager *rewardmanagerbindings.IRewardManager) {
				testContractAddr, testContract := deployRewardManagerTest(t, backend, admin)
				allowlisttest.SetAsEnabled(t, backend, rewardManager, admin, testContractAddr)

				tx, err := testContract.DisableRewards(admin)
				require.NoError(t, err)
				utilstest.WaitReceiptSuccessful(t, backend, tx)

				iter, err := rewardManager.FilterRewardsDisabled(nil, nil)
				require.NoError(t, err)
				defer iter.Close()

				require.True(t, iter.Next(), "expected to find RewardsDisabled event")
				require.Equal(t, testContractAddr, iter.Event.Sender)

				require.False(t, iter.Next(), "expected no more events")
				require.NoError(t, iter.Error())
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			backend := utilstest.NewBackendWithPrecompile(t, rewardmanager.NewConfig(utils.NewUint64(0), []common.Address{adminAddress}, nil, nil, nil), []common.Address{adminAddress, unprivilegedAddress})
			defer backend.Close()

			rewardManager, err := rewardmanagerbindings.NewIRewardManager(rewardmanager.ContractAddress, backend.Client())
			require.NoError(t, err)

			tc.test(t, backend, rewardManager)
		})
	}
}

func TestIAllowList_Events(t *testing.T) {
	precompileCfg := rewardmanager.NewConfig(utils.NewUint64(0), []common.Address{adminAddress}, nil, nil, nil)
	admin := utilstest.NewAuth(t, adminKey, params.TestChainConfig.ChainID)
	allowlisttest.RunAllowListEventTests(t, precompileCfg, rewardmanager.ContractAddress, admin, adminAddress)
}
