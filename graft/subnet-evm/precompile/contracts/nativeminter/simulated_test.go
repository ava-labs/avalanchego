// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nativeminter_test

import (
	"math/big"
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
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/nativeminter"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/utilstest"
	"github.com/ava-labs/avalanchego/utils"

	sim "github.com/ava-labs/avalanchego/graft/subnet-evm/ethclient/simulated"
	nativeminterbindings "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/nativeminter/nativemintertest/bindings"
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

func deployNativeMinterTest(t *testing.T, b *sim.Backend, auth *bind.TransactOpts) (common.Address, *nativeminterbindings.NativeMinterTest) {
	t.Helper()
	addr, tx, contract, err := nativeminterbindings.DeployNativeMinterTest(auth, b.Client(), nativeminter.ContractAddress)
	require.NoError(t, err)
	utilstest.WaitReceiptSuccessful(t, b, tx)
	return addr, contract
}

func TestNativeMinter(t *testing.T) {
	chainID := params.TestChainConfig.ChainID
	admin := utilstest.NewAuth(t, adminKey, chainID)
	unprivileged := utilstest.NewAuth(t, unprivilegedKey, chainID)
	amount := big.NewInt(100)

	type testCase struct {
		name string
		test func(t *testing.T, backend *sim.Backend, nativeMinterIntf *nativeminterbindings.INativeMinter)
	}

	testCases := []testCase{
		{
			name: "admin can mint directly",
			test: func(t *testing.T, backend *sim.Backend, nativeMinter *nativeminterbindings.INativeMinter) {
				testAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")

				initialBalance, err := backend.Client().BalanceAt(t.Context(), testAddr, nil)
				require.NoError(t, err)

				// Admin mints native coins directly to testAddr
				tx, err := nativeMinter.MintNativeCoin(admin, testAddr, amount)
				require.NoError(t, err)
				utilstest.WaitReceiptSuccessful(t, backend, tx)

				// Verify balance increased
				finalBalance, err := backend.Client().BalanceAt(t.Context(), testAddr, nil)
				require.NoError(t, err)
				expectedBalance := new(big.Int).Add(initialBalance, amount)
				require.Zero(t, expectedBalance.Cmp(finalBalance), "balance should have increased by amount")
			},
		},
		{
			name: "unprivileged user cannot mint directly",
			test: func(t *testing.T, _ *sim.Backend, nativeMinter *nativeminterbindings.INativeMinter) {
				testAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")

				// Unprivileged user tries to mint - should fail
				_, err := nativeMinter.MintNativeCoin(unprivileged, testAddr, amount)
				require.ErrorContains(t, err, nativeminter.ErrCannotMint.Error()) //nolint:forbidigo // upstream error wrapped as string
			},
		},
		{
			name: "contract without permission cannot mint",
			test: func(t *testing.T, backend *sim.Backend, nativeMinter *nativeminterbindings.INativeMinter) {
				testContractAddr, testContract := deployNativeMinterTest(t, backend, admin)

				allowlisttest.VerifyRole(t, nativeMinter, testContractAddr, allowlist.NoRole)

				testAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")

				// Contract tries to mint and then should revert because it's not enabled
				_, err := testContract.MintNativeCoin(admin, testAddr, amount)
				require.ErrorContains(t, err, vm.ErrExecutionReverted.Error()) //nolint:forbidigo // upstream error wrapped as string
			},
		},
		{
			name: "contract can be added to allow list",
			test: func(t *testing.T, backend *sim.Backend, nativeMinter *nativeminterbindings.INativeMinter) {
				testContractAddr, _ := deployNativeMinterTest(t, backend, admin)

				allowlisttest.VerifyRole(t, nativeMinter, testContractAddr, allowlist.NoRole)

				allowlisttest.SetAsEnabled(t, backend, nativeMinter, admin, testContractAddr)

				allowlisttest.VerifyRole(t, nativeMinter, testContractAddr, allowlist.EnabledRole)
			},
		},
		{
			name: "enabled contract can mint",
			test: func(t *testing.T, backend *sim.Backend, nativeMinter *nativeminterbindings.INativeMinter) {
				testContractAddr, testContract := deployNativeMinterTest(t, backend, admin)
				testAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")

				allowlisttest.SetAsEnabled(t, backend, nativeMinter, admin, testContractAddr)

				initialBalance, err := backend.Client().BalanceAt(t.Context(), testAddr, nil)
				require.NoError(t, err)

				// Enabled contract mints native coins
				tx, err := testContract.MintNativeCoin(admin, testAddr, amount)
				require.NoError(t, err)
				utilstest.WaitReceiptSuccessful(t, backend, tx)

				// Verify balance increased
				finalBalance, err := backend.Client().BalanceAt(t.Context(), testAddr, nil)
				require.NoError(t, err)
				expectedBalance := new(big.Int).Add(initialBalance, amount)
				require.Zero(t, expectedBalance.Cmp(finalBalance), "balance should have increased by amount")
			},
		},
	}

	precompileCfg := nativeminter.NewConfig(utils.PointerTo[uint64](0), []common.Address{adminAddress}, nil, nil, nil)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			backend := utilstest.NewBackendWithPrecompile(t, precompileCfg, []common.Address{adminAddress, unprivilegedAddress})
			defer backend.Close()

			nativeMinter, err := nativeminterbindings.NewINativeMinter(nativeminter.ContractAddress, backend.Client())
			require.NoError(t, err)

			tc.test(t, backend, nativeMinter)
		})
	}
}

func TestINativeMinter_Events(t *testing.T) {
	chainID := params.TestChainConfig.ChainID
	admin := utilstest.NewAuth(t, adminKey, chainID)
	testKey, _ := crypto.GenerateKey()
	testAddress := crypto.PubkeyToAddress(testKey.PublicKey)

	precompileCfg := nativeminter.NewConfig(utils.PointerTo[uint64](0), []common.Address{adminAddress}, nil, nil, nil)
	backend := utilstest.NewBackendWithPrecompile(t, precompileCfg, []common.Address{adminAddress, unprivilegedAddress})
	defer backend.Close()

	nativeMinter, err := nativeminterbindings.NewINativeMinter(nativeminter.ContractAddress, backend.Client())
	require.NoError(t, err)

	t.Run("should emit NativeCoinMinted event", func(t *testing.T) {
		require := require.New(t)

		amount := big.NewInt(1000)

		tx, err := nativeMinter.MintNativeCoin(admin, testAddress, amount)
		require.NoError(err)
		utilstest.WaitReceiptSuccessful(t, backend, tx)

		// Filter for NativeCoinMinted events
		iter, err := nativeMinter.FilterNativeCoinMinted(
			nil,
			[]common.Address{adminAddress},
			[]common.Address{testAddress},
		)
		require.NoError(err)
		defer iter.Close()

		// Verify event fields match expected values
		require.True(iter.Next(), "expected to find NativeCoinMinted event")
		event := iter.Event
		require.Equal(adminAddress, event.Sender)
		require.Equal(testAddress, event.Recipient)
		require.Zero(amount.Cmp(event.Amount), "amount mismatch")

		// Verify there are no more events
		require.False(iter.Next(), "expected no more NativeCoinMinted events")
		require.NoError(iter.Error())
	})
}
