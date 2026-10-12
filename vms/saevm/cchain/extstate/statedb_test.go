// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/evm/core/state/snapshot"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
)

func TestMain(m *testing.M) {
	customtypes.Register()
	params.RegisterExtras()
	RegisterExtras()
	os.Exit(m.Run())
}

func newEmptyStateDB(tb testing.TB) *StateDB {
	tb.Helper()
	s, err := state.New(types.EmptyRootHash, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
	require.NoError(tb, err, "state.New()")
	return New(s)
}

func TestStateKeyPartition(t *testing.T) {
	allOnes := common.Hash{
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	}
	allOnesExceptBit0 := allOnes
	allOnesExceptBit0[0] = 0xfe

	tests := []struct {
		name         string
		key          common.Hash
		wantCoinID   common.Hash
		wantStateKey common.Hash
	}{
		{
			name:         "all_zero",
			wantCoinID:   common.Hash{0x01},
			wantStateKey: common.Hash{0x00},
		},
		{
			name:         "bit_0_set",
			key:          common.Hash{0x0f},
			wantCoinID:   common.Hash{0x0f},
			wantStateKey: common.Hash{0x0e},
		},
		{
			name:         "bit_0_clear",
			key:          common.Hash{0xf0},
			wantCoinID:   common.Hash{0xf1},
			wantStateKey: common.Hash{0xf0},
		},
		{
			name:         "first_byte_all_ones",
			key:          common.Hash{0xff},
			wantCoinID:   common.Hash{0xff},
			wantStateKey: common.Hash{0xfe},
		},
		{
			name:         "all_ones",
			key:          allOnes,
			wantCoinID:   allOnes,
			wantStateKey: allOnesExceptBit0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coinID := tt.key
			normalizeCoinID(&coinID)
			assert.Equalf(t, tt.wantCoinID, coinID, "normalizeCoinID(%s)", tt.key)

			stateKey := tt.key
			normalizeStateKey(&stateKey)
			assert.Equalf(t, tt.wantStateKey, stateKey, "normalizeStateKey(%s)", tt.key)
		})
	}
}

func FuzzStateKeyPartition(f *testing.F) {
	f.Fuzz(func(t *testing.T, keyBytes []byte) {
		var key common.Hash
		copy(key[:], keyBytes)

		coinID, stateKey := key, key
		normalizeCoinID(&coinID)
		normalizeStateKey(&stateKey)
		require.NotEqualf(t, coinID, stateKey, "normalizeCoinID(%s) == normalizeStateKey(%s)", key, key)
		// Only the first byte is used to partition the key space.
		require.Equalf(t, key[1:], coinID[1:], "normalizeCoinID(%s)[1:]", key)
		require.Equalf(t, key[1:], stateKey[1:], "normalizeStateKey(%s)[1:]", key)
	})
}

// TestStorageDoesNotOverwriteMultiCoinBalance covers the hook installed by
// [RegisterExtras]. Without it, a storage write to a key equal to a coin ID
// would overwrite that coin's balance.
func TestStorageDoesNotOverwriteMultiCoinBalance(t *testing.T) {
	s := newEmptyStateDB(t)
	addr := common.Address{1}
	coinID := common.Hash{0x01} // Already normalized.

	want := big.NewInt(10)
	s.AddBalanceMultiCoin(addr, coinID, want)
	stored := common.Hash{0xff}
	s.SetState(addr, coinID, stored)

	got := s.GetBalanceMultiCoin(addr, coinID)
	assert.Zerof(t, want.Cmp(got), "%T.GetBalanceMultiCoin() = %d, want %d", s, got, want)
	assert.Equalf(t, stored, s.GetState(addr, coinID), "%T.GetState()", s)
}

func TestMultiCoinBalance(t *testing.T) {
	tests := []struct {
		name          string
		deltas        []int64 // Positive deltas are added and negative ones subtracted.
		want          int64
		wantMultiCoin bool
	}{
		{
			name:   "add_zero",
			deltas: []int64{0},
		},
		{
			name:          "add",
			deltas:        []int64{10},
			want:          10,
			wantMultiCoin: true,
		},
		{
			name:          "add_then_sub",
			deltas:        []int64{10, -4},
			want:          6,
			wantMultiCoin: true,
		},
		{
			name:          "sub_entire_balance",
			deltas:        []int64{10, -10},
			wantMultiCoin: true,
		},
		{
			name:          "add_sub_add",
			deltas:        []int64{10, -5, 3},
			want:          8,
			wantMultiCoin: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newEmptyStateDB(t)
			addr := common.Address{1}
			coinID := common.Hash{2}
			for _, d := range tt.deltas {
				if d < 0 {
					s.SubBalanceMultiCoin(addr, coinID, big.NewInt(-d))
				} else {
					s.AddBalanceMultiCoin(addr, coinID, big.NewInt(d))
				}
			}

			got := s.GetBalanceMultiCoin(addr, coinID)
			assert.Zerof(t, big.NewInt(tt.want).Cmp(got), "%T.GetBalanceMultiCoin() = %d, want %d", s, got, tt.want)
			assert.Truef(t, s.Exist(addr), "%T.Exist()", s)
			assert.Equalf(t, tt.wantMultiCoin, customtypes.IsMultiCoin(s.StateDB, addr), "customtypes.IsMultiCoin()")
		})
	}
}

func TestSubBalanceMultiCoinZero(t *testing.T) {
	s := newEmptyStateDB(t)
	addr := common.Address{1}
	s.SubBalanceMultiCoin(addr, common.Hash{1}, new(big.Int))

	assert.Falsef(t, s.Exist(addr), "%T.Exist(%s)", s, addr)
}

func TestMultiCoinSnapshot(t *testing.T) {
	diskdb := rawdb.NewMemoryDatabase()
	db := state.NewDatabase(diskdb)
	// The empty root doubles as the block hash of the disk layer.
	snaps := snapshot.NewTestTree(diskdb, types.EmptyRootHash, types.EmptyRootHash)

	var (
		addr   = common.Address{1}
		asset1 = common.Hash{1}
		asset2 = common.Hash{2}
	)
	open := func(t *testing.T, root common.Hash) *StateDB {
		t.Helper()
		s, err := state.New(root, db, snaps)
		require.NoErrorf(t, err, "state.New(%s)", root)
		return New(s)
	}
	snapshotOpts := stateconf.WithSnapshotUpdateOpts(snapshot.WithBlockHashes(common.Hash{}, common.Hash{}))
	commit := func(t *testing.T, s *StateDB) common.Hash {
		t.Helper()
		root, err := s.Commit(0, false, snapshotOpts)
		require.NoErrorf(t, err, "%T.Commit()", s)
		return root
	}
	requireBalances := func(t *testing.T, s *StateDB, wantRegular uint64, wantAsset1, wantAsset2 int64) {
		t.Helper()
		require.Equalf(t, uint256.NewInt(wantRegular), s.GetBalance(addr), "%T.GetBalance()", s)
		for asset, want := range map[common.Hash]int64{
			asset1: wantAsset1,
			asset2: wantAsset2,
		} {
			got := s.GetBalanceMultiCoin(addr, asset)
			require.Zerof(t, big.NewInt(want).Cmp(got), "%T.GetBalanceMultiCoin(%s) = %d, want %d", s, asset, got, want)
		}
	}

	s := open(t, types.EmptyRootHash)
	requireBalances(t, s, 0, 0, 0)
	s.AddBalance(addr, uint256.NewInt(10))
	root := commit(t, s)
	requireBalances(t, s, 10, 0, 0)

	s = open(t, root)
	s.AddBalanceMultiCoin(addr, asset1, big.NewInt(10))
	root = commit(t, s)
	requireBalances(t, s, 10, 10, 0)

	// [state.StateDB.Commit] caps the snapshot at 128 diff layers, so 256
	// commits flatten the oldest layers into the disk layer.
	for range 256 {
		s = open(t, root)
		s.AddBalanceMultiCoin(addr, asset1, big.NewInt(1))
		s.AddBalanceMultiCoin(addr, asset2, big.NewInt(2))
		root = commit(t, s)
	}
	requireBalances(t, s, 10, 266, 512)

	// The regular balance is now only in the disk layer.
	s = open(t, root)
	s.AddBalance(addr, uint256.NewInt(1))
	s.AddBalanceMultiCoin(addr, asset1, big.NewInt(1))
	root = commit(t, s)
	requireBalances(t, open(t, root), 11, 267, 512)
}

func TestMultiCoinSnapshotGeneration(t *testing.T) {
	diskdb := rawdb.NewMemoryDatabase()
	db := state.NewDatabase(diskdb)

	addr := common.BytesToAddress([]byte("addr1"))
	coinID := common.BytesToHash([]byte("coin1"))
	want := big.NewInt(10)

	statedb, err := state.New(types.EmptyRootHash, db, nil)
	require.NoError(t, err, "state.New()")
	s := New(statedb)
	s.AddBalanceMultiCoin(addr, coinID, want)
	root, err := s.Commit(0, false)
	require.NoErrorf(t, err, "%T.Commit()", s)
	triedb := db.TrieDB()
	require.NoErrorf(t, triedb.Commit(root, true), "%T.Commit()", triedb)

	snaps, err := snapshot.New(
		snapshot.Config{
			CacheSize:  16,
			SkipVerify: true,
		},
		diskdb,
		triedb,
		common.Hash{},
		root,
	)
	require.NoError(t, err, "snapshot.New()")
	snap := snaps.Snapshot(root)

	addrHash := crypto.Keccak256Hash(addr[:])
	account, err := snap.Account(addrHash)
	require.NoErrorf(t, err, "%T.Account()", snap)
	require.True(t, customtypes.IsAccountMultiCoin(account), "customtypes.IsAccountMultiCoin()")

	normalizeCoinID(&coinID)
	storage, err := snap.Storage(addrHash, crypto.Keccak256Hash(coinID[:]))
	require.NoErrorf(t, err, "%T.Storage()", snap)
	got := new(big.Int).SetBytes(storage)
	require.Zerof(t, want.Cmp(got), "%T.Storage() = %d, want %d", snap, got, want)
}

func TestPrepare(t *testing.T) {
	var (
		addrA        = common.Address{1}
		addrB        = common.Address{2}
		noPredicater = common.Address{3}
		sender       = common.Address{4}
		transientKey = common.Hash{5}
		keysA0       = []common.Hash{{0xa0}}
		keysA1       = []common.Hash{{0xa1}}
		keysB0       = []common.Hash{{0xb0}}
	)
	rules := params.TestChainConfig.Rules(common.Big0, params.IsMergeTODO, 0)
	predicaters := params.GetRulesExtra(rules).Predicaters
	predicaters[addrA] = nil
	predicaters[addrB] = nil

	s := newEmptyStateDB(t)
	s.SetTransientState(addrA, transientKey, common.Hash{1})
	s.Prepare(
		rules,
		sender,
		common.Address{},
		nil,
		nil,
		types.AccessList{
			{Address: addrA, StorageKeys: keysA0},
			{Address: noPredicater, StorageKeys: []common.Hash{{0xc0}}},
			{Address: addrB, StorageKeys: keysB0},
			{Address: addrA, StorageKeys: keysA1},
		},
	)
	assert.Truef(t, s.AddressInAccessList(sender), "%T.AddressInAccessList(%s)", s, sender)
	assert.Zerof(t, s.GetTransientState(addrA, transientKey), "%T.GetTransientState(%s, %s)", s, addrA, transientKey)

	tests := []struct {
		name   string
		addr   common.Address
		index  int
		want   predicate.Predicate
		wantOK bool
	}{
		{
			name:   "first_for_address",
			addr:   addrA,
			want:   keysA0,
			wantOK: true,
		},
		{
			name:   "second_for_address",
			addr:   addrA,
			index:  1,
			want:   keysA1,
			wantOK: true,
		},
		{
			name:   "other_address",
			addr:   addrB,
			want:   keysB0,
			wantOK: true,
		},
		{
			name:  "index_past_end",
			addr:  addrA,
			index: 2,
		},
		{
			name:  "negative_index",
			addr:  addrA,
			index: -1,
		},
		{
			name: "address_without_predicater",
			addr: noPredicater,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := s.GetPredicate(tt.addr, tt.index)
			assert.Equalf(t, tt.want, got, "%T.GetPredicate(%s, %d)", s, tt.addr, tt.index)
			assert.Equalf(t, tt.wantOK, ok, "%T.GetPredicate(%s, %d) ok", s, tt.addr, tt.index)
		})
	}

	// A new transaction MUST NOT see predicates from the previous transaction.
	s.Prepare(
		rules,
		common.Address{},
		common.Address{},
		nil,
		nil,
		nil,
	)
	got, ok := s.GetPredicate(addrA, 0)
	assert.Nilf(t, got, "%T.GetPredicate(%s, 0)", s, addrA)
	assert.Falsef(t, ok, "%T.GetPredicate(%s, 0) ok", s, addrA)
}
