// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extstate

import (
	"math/big"
	"os"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/core/state/snapshot"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
)

func TestMain(m *testing.M) {
	customtypes.Register()
	os.Exit(m.Run())
}

func TestMultiCoinOperations(t *testing.T) {
	memdb := rawdb.NewMemoryDatabase()
	db := state.NewDatabase(memdb)
	statedb, err := state.New(types.EmptyRootHash, db, nil)
	require.NoError(t, err, "creating empty statedb")

	addr := common.Address{1}
	assetID := common.Hash{2}

	statedb.AddBalance(addr, new(uint256.Int))

	wrappedStateDB := New(statedb)
	balance := wrappedStateDB.GetBalanceMultiCoin(addr, assetID)
	require.Equal(t, "0", balance.String(), "expected zero big.Int multicoin balance as string")

	wrappedStateDB.AddBalanceMultiCoin(addr, assetID, big.NewInt(10))
	wrappedStateDB.SubBalanceMultiCoin(addr, assetID, big.NewInt(5))
	wrappedStateDB.AddBalanceMultiCoin(addr, assetID, big.NewInt(3))

	balance = wrappedStateDB.GetBalanceMultiCoin(addr, assetID)
	require.Equal(t, "8", balance.String(), "unexpected multicoin balance string")
}

func TestMultiCoinSnapshot(t *testing.T) {
	memdb := rawdb.NewMemoryDatabase()
	db := state.NewDatabase(memdb)

	// Create empty [snapshot.Tree] and [StateDB]
	root := common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")
	// Use the root as both the stateRoot and blockHash for this test.
	snapTree := snapshot.NewTestTree(memdb, root, root)

	addr := common.Address{1}
	assetID1 := common.Hash{1}
	assetID2 := common.Hash{2}
	assertBalances := func(t *testing.T, wrappedStateDB *StateDB, regular, multicoin1, multicoin2 int64) {
		t.Helper()

		balance := wrappedStateDB.GetBalance(addr)
		require.Equal(t, uint256.NewInt(uint64(regular)), balance, "incorrect non-multicoin balance")
		balanceBig := wrappedStateDB.GetBalanceMultiCoin(addr, assetID1)
		require.Equal(t, big.NewInt(multicoin1).String(), balanceBig.String(), "incorrect multicoin1 balance")
		balanceBig = wrappedStateDB.GetBalanceMultiCoin(addr, assetID2)
		require.Equal(t, big.NewInt(multicoin2).String(), balanceBig.String(), "incorrect multicoin2 balance")
	}

	// Create new state
	statedb, err := state.New(root, db, snapTree)
	require.NoError(t, err, "creating statedb")

	wrappedStateDB := New(statedb)
	assertBalances(t, wrappedStateDB, 0, 0, 0)

	wrappedStateDB.AddBalance(addr, uint256.NewInt(10))
	assertBalances(t, wrappedStateDB, 10, 0, 0)

	// Commit and get the new root
	snapshotOpt := snapshot.WithBlockHashes(common.Hash{}, common.Hash{})
	root, err = wrappedStateDB.Commit(0, false, stateconf.WithSnapshotUpdateOpts(snapshotOpt))
	require.NoError(t, err, "committing statedb")
	assertBalances(t, wrappedStateDB, 10, 0, 0)

	// Create a new state from the latest root, add a multicoin balance, and
	// commit it to the tree.
	statedb, err = state.New(root, db, snapTree)
	require.NoError(t, err, "creating statedb")

	wrappedStateDB = New(statedb)
	wrappedStateDB.AddBalanceMultiCoin(addr, assetID1, big.NewInt(10))
	snapshotOpt = snapshot.WithBlockHashes(common.Hash{}, common.Hash{})
	root, err = wrappedStateDB.Commit(0, false, stateconf.WithSnapshotUpdateOpts(snapshotOpt))
	require.NoError(t, err, "committing statedb")
	assertBalances(t, wrappedStateDB, 10, 10, 0)

	// Add more layers than the cap and ensure the balances and layers are correct
	for i := 0; i < 256; i++ {
		statedb, err = state.New(root, db, snapTree)
		require.NoErrorf(t, err, "creating statedb %d", i)

		wrappedStateDB = New(statedb)
		wrappedStateDB.AddBalanceMultiCoin(addr, assetID1, big.NewInt(1))
		wrappedStateDB.AddBalanceMultiCoin(addr, assetID2, big.NewInt(2))
		snapshotOpt = snapshot.WithBlockHashes(common.Hash{}, common.Hash{})
		root, err = wrappedStateDB.Commit(0, false, stateconf.WithSnapshotUpdateOpts(snapshotOpt))
		require.NoErrorf(t, err, "committing statedb %d", i)
	}
	assertBalances(t, wrappedStateDB, 10, 266, 512)

	// Do one more add, including the regular balance which is now in the
	// collapsed snapshot
	statedb, err = state.New(root, db, snapTree)
	require.NoError(t, err, "creating statedb")

	wrappedStateDB = New(statedb)
	wrappedStateDB.AddBalance(addr, uint256.NewInt(1))
	wrappedStateDB.AddBalanceMultiCoin(addr, assetID1, big.NewInt(1))
	snapshotOpt = snapshot.WithBlockHashes(common.Hash{}, common.Hash{})
	root, err = wrappedStateDB.Commit(0, false, stateconf.WithSnapshotUpdateOpts(snapshotOpt))
	require.NoError(t, err, "committing statedb")

	statedb, err = state.New(root, db, snapTree)
	require.NoError(t, err, "creating statedb")

	wrappedStateDB = New(statedb)
	assertBalances(t, wrappedStateDB, 11, 267, 512)
}

func TestGenerateMultiCoinAccounts(t *testing.T) {
	diskdb := rawdb.NewMemoryDatabase()
	database := state.NewDatabase(diskdb)

	addr := common.BytesToAddress([]byte("addr1"))
	addrHash := crypto.Keccak256Hash(addr[:])

	assetID := common.BytesToHash([]byte("coin1"))
	assetBalance := big.NewInt(10)

	statedb, err := state.New(common.Hash{}, database, nil)
	require.NoError(t, err, "creating statedb")

	wrappedStateDB := New(statedb)
	wrappedStateDB.AddBalanceMultiCoin(addr, assetID, assetBalance)
	root, err := wrappedStateDB.Commit(0, false)
	require.NoError(t, err, "committing statedb")

	triedb := database.TrieDB()
	err = triedb.Commit(root, true)
	require.NoError(t, err, "committing trie")

	// Build snapshot from scratch
	snapConfig := snapshot.Config{
		CacheSize:  16,
		AsyncBuild: false,
		NoBuild:    false,
		SkipVerify: true,
	}
	snaps, err := snapshot.New(snapConfig, diskdb, triedb, common.Hash{}, root)
	require.NoError(t, err, "rebuilding snapshot")

	// Get latest snapshot and make sure it has the correct account and storage
	snap := snaps.Snapshot(root)
	snapAccount, err := snap.Account(addrHash)
	require.NoError(t, err, "getting account from snapshot")
	require.True(t, customtypes.IsAccountMultiCoin(snapAccount), "snap account must be multi-coin")

	normalizeCoinID(&assetID)
	assetHash := crypto.Keccak256Hash(assetID.Bytes())
	storageBytes, err := snap.Storage(addrHash, assetHash)
	require.NoError(t, err, "getting storage from snapshot")

	actualAssetBalance := new(big.Int).SetBytes(storageBytes)
	require.Equal(t, assetBalance, actualAssetBalance, "incorrect asset balance")
}
