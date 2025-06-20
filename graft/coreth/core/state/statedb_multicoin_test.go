// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

func TestMultiCoinOperations(t *testing.T) {
	s := newStateEnv()
	addr := common.Address{1}
	assetID := common.Hash{2}

	root, err := s.state.Commit(0, false)
	require.NoError(t, err, "committing state")
	s.state, err = New(root, s.state.db, s.state.snaps)
	require.NoError(t, err, "creating statedb")

	s.state.AddBalance(addr, new(uint256.Int))

	balance := s.state.GetBalanceMultiCoin(addr, assetID)
	require.Equal(t, "0", balance.String(), "expected zero big.Int multicoin balance as string")

	s.state.AddBalanceMultiCoin(addr, assetID, big.NewInt(10))
	s.state.SubBalanceMultiCoin(addr, assetID, big.NewInt(5))
	s.state.AddBalanceMultiCoin(addr, assetID, big.NewInt(3))

	balance = s.state.GetBalanceMultiCoin(addr, assetID)
	require.Equal(t, "8", balance.String(), "unexpected multicoin balance string")
}

func TestMultiCoinSnapshot(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	sdb := NewDatabase(db)

	// Create empty [snapshot.Tree] and [StateDB]
	root := common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")
	// Use the root as both the stateRoot and blockHash for this test.
	snapTree := snapshot.NewTestTree(db, root, root)

	addr := common.Address{1}
	assetID1 := common.Hash{1}
	assetID2 := common.Hash{2}

	var stateDB *StateDB
	assertBalances := func(regular, multicoin1, multicoin2 int64) {
		balance := stateDB.GetBalance(addr)
		require.Equal(t, uint256.NewInt(uint64(regular)), balance, "incorrect non-multicoin balance")
		balanceBig := stateDB.GetBalanceMultiCoin(addr, assetID1)
		require.Equal(t, big.NewInt(multicoin1).String(), balanceBig.String(), "incorrect multicoin1 balance")
		balanceBig = stateDB.GetBalanceMultiCoin(addr, assetID2)
		require.Equal(t, big.NewInt(multicoin2).String(), balanceBig.String(), "incorrect multicoin2 balance")
	}

	// Create new state
	stateDB, err := New(root, sdb, snapTree)
	require.NoError(t, err, "creating statedb")
	assertBalances(0, 0, 0)

	stateDB.AddBalance(addr, uint256.NewInt(10))
	assertBalances(10, 0, 0)

	// Commit and get the new root
	snapshotOpt := snapshot.WithBlockHashes(common.Hash{}, common.Hash{})
	root, err = stateDB.Commit(0, false, stateconf.WithSnapshotUpdateOpts(snapshotOpt))
	require.NoError(t, err, "committing statedb")
	assertBalances(10, 0, 0)

	// Create a new state from the latest root, add a multicoin balance, and
	// commit it to the tree.
	stateDB, err = New(root, sdb, snapTree)
	require.NoError(t, err, "creating statedb")
	stateDB.AddBalanceMultiCoin(addr, assetID1, big.NewInt(10))
	snapshotOpt = snapshot.WithBlockHashes(common.Hash{}, common.Hash{})
	root, err = stateDB.Commit(0, false, stateconf.WithSnapshotUpdateOpts(snapshotOpt))
	require.NoError(t, err, "committing statedb")
	assertBalances(10, 10, 0)

	// Add more layers than the cap and ensure the balances and layers are correct
	for i := 0; i < 256; i++ {
		stateDB, err = New(root, sdb, snapTree)
		require.NoErrorf(t, err, "creating statedb %d", i)
		stateDB.AddBalanceMultiCoin(addr, assetID1, big.NewInt(1))
		stateDB.AddBalanceMultiCoin(addr, assetID2, big.NewInt(2))
		snapshotOpt = snapshot.WithBlockHashes(common.Hash{}, common.Hash{})
		root, err = stateDB.Commit(0, false, stateconf.WithSnapshotUpdateOpts(snapshotOpt))
		require.NoErrorf(t, err, "committing statedb %d", i)
	}
	assertBalances(10, 266, 512)

	// Do one more add, including the regular balance which is now in the
	// collapsed snapshot
	stateDB, err = New(root, sdb, snapTree)
	require.NoError(t, err, "creating statedb")
	stateDB.AddBalance(addr, uint256.NewInt(1))
	stateDB.AddBalanceMultiCoin(addr, assetID1, big.NewInt(1))
	snapshotOpt = snapshot.WithBlockHashes(common.Hash{}, common.Hash{})
	root, err = stateDB.Commit(0, false, stateconf.WithSnapshotUpdateOpts(snapshotOpt))
	require.NoError(t, err, "committing statedb")

	stateDB, err = New(root, sdb, snapTree)
	require.NoError(t, err, "creating statedb")
	assertBalances(11, 267, 512)
}

func TestGenerateMultiCoinAccounts(t *testing.T) {
	diskdb := rawdb.NewMemoryDatabase()
	database := NewDatabase(diskdb)

	addr := common.BytesToAddress([]byte("addr1"))
	addrHash := crypto.Keccak256Hash(addr[:])

	assetID := common.BytesToHash([]byte("coin1"))
	assetBalance := big.NewInt(10)

	stateDB, err := New(common.Hash{}, database, nil)
	require.NoError(t, err, "creating statedb")
	stateDB.AddBalanceMultiCoin(addr, assetID, assetBalance)
	root, err := stateDB.Commit(0, false)
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
	require.True(t, customtypes.IsMultiCoin(snapAccount), "snap account must be multi-coin")

	NormalizeCoinID(&assetID)
	assetHash := crypto.Keccak256Hash(assetID.Bytes())
	storageBytes, err := snap.Storage(addrHash, assetHash)
	require.NoError(t, err, "getting storage from snapshot")

	actualAssetBalance := new(big.Int).SetBytes(storageBytes)
	require.Equal(t, assetBalance, actualAssetBalance, "incorrect asset balance")
}
