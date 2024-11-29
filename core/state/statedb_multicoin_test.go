// (c) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/rlp"
	"github.com/holiman/uint256"
)

func TestMultiCoinOperations(t *testing.T) {
	s := newStateEnv()
	addr := common.Address{1}
	assetID := common.Hash{2}

	root, _ := s.state.Commit(0, false)
	s.state, _ = New(root, s.state.db, s.state.snaps)

	s.state.AddBalance(addr, new(uint256.Int))

	balance := s.state.GetBalanceMultiCoin(addr, assetID)
	if balance.Cmp(big.NewInt(0)) != 0 {
		t.Fatal("expected zero multicoin balance")
	}

	s.state.AddBalanceMultiCoin(addr, assetID, big.NewInt(10))
	s.state.SubBalanceMultiCoin(addr, assetID, big.NewInt(5))
	s.state.AddBalanceMultiCoin(addr, assetID, big.NewInt(3))

	balance = s.state.GetBalanceMultiCoin(addr, assetID)
	if balance.Cmp(big.NewInt(8)) != 0 {
		t.Fatal("expected multicoin balance to be 8")
	}
}

func TestMultiCoinSnapshot(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	sdb := NewDatabase(db)

	// Create empty snapshot.Tree and StateDB
	root := common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")
	// Use the root as both the stateRoot and blockHash for this test.
	snapTree := snapshot.NewTestTree(db, root, root)

	addr := common.Address{1}
	assetID1 := common.Hash{1}
	assetID2 := common.Hash{2}

	var stateDB *StateDB
	assertBalances := func(regular, multicoin1, multicoin2 int64) {
		balance := stateDB.GetBalance(addr)
		if balance.Cmp(uint256.NewInt(uint64(regular))) != 0 {
			t.Fatal("incorrect non-multicoin balance")
		}
		balanceBig := stateDB.GetBalanceMultiCoin(addr, assetID1)
		if balanceBig.Cmp(big.NewInt(multicoin1)) != 0 {
			t.Fatal("incorrect multicoin1 balance")
		}
		balanceBig = stateDB.GetBalanceMultiCoin(addr, assetID2)
		if balanceBig.Cmp(big.NewInt(multicoin2)) != 0 {
			t.Fatal("incorrect multicoin2 balance")
		}
	}

	// Create new state
	stateDB, _ = New(root, sdb, snapTree)
	assertBalances(0, 0, 0)

	stateDB.AddBalance(addr, uint256.NewInt(10))
	assertBalances(10, 0, 0)

	// Commit and get the new root
	root, _ = stateDB.Commit(0, false, snapshot.WithBlockHashes(common.Hash{}, common.Hash{}))
	assertBalances(10, 0, 0)

	// Create a new state from the latest root, add a multicoin balance, and
	// commit it to the tree.
	stateDB, _ = New(root, sdb, snapTree)
	stateDB.AddBalanceMultiCoin(addr, assetID1, big.NewInt(10))
	root, _ = stateDB.Commit(0, false, snapshot.WithBlockHashes(common.Hash{}, common.Hash{}))
	assertBalances(10, 10, 0)

	// Add more layers than the cap and ensure the balances and layers are correct
	for i := 0; i < 256; i++ {
		stateDB, _ = New(root, sdb, snapTree)
		stateDB.AddBalanceMultiCoin(addr, assetID1, big.NewInt(1))
		stateDB.AddBalanceMultiCoin(addr, assetID2, big.NewInt(2))
		root, _ = stateDB.Commit(0, false, snapshot.WithBlockHashes(common.Hash{}, common.Hash{}))
	}
	assertBalances(10, 266, 512)

	// Do one more add, including the regular balance which is now in the
	// collapsed snapshot
	stateDB, _ = New(root, sdb, snapTree)
	stateDB.AddBalance(addr, uint256.NewInt(1))
	stateDB.AddBalanceMultiCoin(addr, assetID1, big.NewInt(1))
	root, _ = stateDB.Commit(0, false, snapshot.WithBlockHashes(common.Hash{}, common.Hash{}))
	stateDB, _ = New(root, sdb, snapTree)
	assertBalances(11, 267, 512)
}

func TestGenerateMultiCoinAccounts(t *testing.T) {
	var (
		diskdb   = rawdb.NewMemoryDatabase()
		database = NewDatabase(diskdb)

		addr     = common.BytesToAddress([]byte("addr1"))
		addrHash = crypto.Keccak256Hash(addr[:])

		assetID      = common.BytesToHash([]byte("coin1"))
		assetBalance = big.NewInt(10)
	)

	stateDB, err := New(common.Hash{}, database, nil)
	if err != nil {
		t.Fatal(err)
	}
	stateDB.AddBalanceMultiCoin(addr, assetID, assetBalance)
	root, err := stateDB.Commit(0, false)
	if err != nil {
		t.Fatal(err)
	}

	triedb := database.TrieDB()
	if err := triedb.Commit(root, true); err != nil {
		t.Fatal(err)
	}
	// Build snapshot from scratch
	snapConfig := snapshot.Config{
		CacheSize:  16,
		AsyncBuild: false,
		NoBuild:    false,
		SkipVerify: true,
	}
	snaps, err := snapshot.New(snapConfig, diskdb, triedb, common.Hash{}, root)
	if err != nil {
		t.Error("Unexpected error while rebuilding snapshot:", err)
	}

	// Get latest snapshot and make sure it has the correct account and storage
	snap := snaps.Snapshot(root)
	snapAccount, err := snap.AccountRLP(addrHash)
	if err != nil {
		t.Fatal(err)
	}
	account := new(types.StateAccount)
	if err := rlp.DecodeBytes(snapAccount, account); err != nil {
		t.Fatal(err)
	}
	if !types.IsMultiCoin(account) {
		t.Fatalf("Expected SnapAccount to return IsMultiCoin: true, found: %v", types.IsMultiCoin(account))
	}

	NormalizeCoinID(&assetID)
	assetHash := crypto.Keccak256Hash(assetID.Bytes())
	storageBytes, err := snap.Storage(addrHash, assetHash)
	if err != nil {
		t.Fatal(err)
	}

	actualAssetBalance := new(big.Int).SetBytes(storageBytes)
	if actualAssetBalance.Cmp(assetBalance) != 0 {
		t.Fatalf("Expected asset balance: %v, found %v", assetBalance, actualAssetBalance)
	}
}
