// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customrawdb

import (
	"math/big"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/require"
)

func TestClearAllSyncSegments(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	// add a key that should be cleared
	require.NoError(t, WriteSyncSegment(db, common.Hash{1}, common.Hash{}))

	// add a key that should not be cleared
	key := slices.Concat(syncSegmentsPrefix, []byte("foo"))
	require.NoError(t, db.Put(key, []byte("bar")))

	require.NoError(t, ClearAllSyncSegments(db))

	count := 0
	it := db.NewIterator(syncSegmentsPrefix, nil)
	defer it.Release()
	for it.Next() {
		count++
	}
	require.NoError(t, it.Error())
	require.Equal(t, 1, count)
}

func TestWriteReadSyncRoot(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	// No root written yet
	root, err := ReadSyncRoot(db)
	require.NoError(t, err)
	require.Equal(t, common.Hash{}, root)

	// Write and read back
	want := common.HexToHash("0x01")
	require.NoError(t, WriteSyncRoot(db, want))
	got, err := ReadSyncRoot(db)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestCodeToFetchIteratorAndDelete(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	h1 := common.HexToHash("0x11")
	h2 := common.HexToHash("0x22")

	WriteCodeToFetch(db, h1)
	WriteCodeToFetch(db, h2)

	// Insert a malformed key that should be ignored by the iterator (wrong length)
	bad := append(append([]byte{}, CodeToFetchPrefix...), append(h1.Bytes(), 0x00)...)
	require.NoError(t, db.Put(bad, []byte("x")))

	seen := map[common.Hash]bool{}
	it := NewCodeToFetchIterator(db)
	defer it.Release()
	for it.Next() {
		key := it.Key()
		// Last common.HashLength bytes are the code hash
		got := common.BytesToHash(key[len(CodeToFetchPrefix):])
		seen[got] = true
	}
	require.True(t, seen[h1])
	require.True(t, seen[h2])

	// Delete one and confirm only one remains
	DeleteCodeToFetch(db, h1)
	count := 0
	it = NewCodeToFetchIterator(db)
	defer it.Release()
	for it.Next() {
		count++
	}
	require.Equal(t, 1, count)
}

func TestSyncSegmentsIteratorUnpackAndClear(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	rootA := common.HexToHash("0xaaa")
	rootB := common.HexToHash("0xbbb")
	start1 := common.HexToHash("0x01")
	start2 := common.HexToHash("0x02")
	start3 := common.HexToHash("0x03")

	require.NoError(t, WriteSyncSegment(db, rootA, start1))
	require.NoError(t, WriteSyncSegment(db, rootA, start2))
	require.NoError(t, WriteSyncSegment(db, rootB, start3))

	// Iterate only over rootA
	it := NewSyncSegmentsIterator(db, rootA)
	defer it.Release()
	gotStarts := map[common.Hash]bool{}
	for it.Next() {
		root, start := UnpackSyncSegmentKey(it.Key())
		require.Equal(t, rootA, root)
		gotStarts[common.BytesToHash(start)] = true
	}
	require.True(t, gotStarts[start1])
	require.True(t, gotStarts[start2])
	require.False(t, gotStarts[start3])

	// Clear only rootA
	require.NoError(t, ClearSyncSegments(db, rootA))
	it = NewSyncSegmentsIterator(db, rootA)
	defer it.Release()
	count := 0
	for it.Next() {
		count++
	}
	require.Equal(t, 0, count)

	// RootB remains
	it = NewSyncSegmentsIterator(db, rootB)
	defer it.Release()
	count = 0
	for it.Next() {
		count++
	}
	require.Equal(t, 1, count)
}

func TestStorageTriesIteratorUnpackAndClear(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	root := common.HexToHash("0xabc")
	acct1 := common.HexToHash("0x01")
	acct2 := common.HexToHash("0x02")

	require.NoError(t, WriteSyncStorageTrie(db, root, acct1))
	require.NoError(t, WriteSyncStorageTrie(db, root, acct2))

	it := NewSyncStorageTriesIterator(db, nil)
	defer it.Release()
	seen := map[common.Hash]bool{}
	for it.Next() {
		r, a := UnpackSyncStorageTrieKey(it.Key())
		require.Equal(t, root, r)
		seen[a] = true
	}
	require.True(t, seen[acct1])
	require.True(t, seen[acct2])

	require.NoError(t, ClearSyncStorageTrie(db, root))
	it = NewSyncStorageTriesIterator(db, nil)
	defer it.Release()
	count := 0
	for it.Next() {
		count++
	}
	require.Equal(t, 0, count)
}

func TestClearAllSyncStorageTries(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	root := common.HexToHash("0xabc")
	// Keys that should be cleared
	require.NoError(t, WriteSyncStorageTrie(db, root, common.HexToHash("0x01")))
	require.NoError(t, WriteSyncStorageTrie(db, root, common.HexToHash("0x02")))
	// Key that should not be cleared due to wrong length.
	bad := make([]byte, 0, len(syncStorageTriesPrefix)+2*common.HashLength+1)
	bad = append(bad, syncStorageTriesPrefix...)
	bad = append(bad, root.Bytes()...)
	bad = append(bad, common.HexToHash("0xff").Bytes()...)
	bad = append(bad, byte(0x00))
	require.NoError(t, db.Put(bad, []byte("x")))

	require.NoError(t, ClearAllSyncStorageTries(db))

	// Only the malformed key should remain
	count := 0
	it := db.NewIterator(syncStorageTriesPrefix, nil)
	defer it.Release()
	for it.Next() {
		count++
	}
	require.NoError(t, it.Error())
	require.Equal(t, 1, count)
}

func TestSyncPerformedAndLatest(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	require.NoError(t, WriteSyncPerformed(db, 10))
	require.NoError(t, WriteSyncPerformed(db, 20))
	require.NoError(t, WriteSyncPerformed(db, 15))

	// Iterator yields all
	it := NewSyncPerformedIterator(db)
	defer it.Release()
	var vals []uint64
	for it.Next() {
		vals = append(vals, UnpackSyncPerformedKey(it.Key()))
	}
	require.ElementsMatch(t, []uint64{10, 20, 15}, vals)

	// Latest is max
	require.Equal(t, uint64(20), GetLatestSyncPerformed(db))
}

func TestGetLatestSyncPerformedEmpty(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	require.Equal(t, uint64(0), GetLatestSyncPerformed(db))
}

func TestChainConfigReadWriteWithUpgrade(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	type upgradeCfg struct {
		X int `json:"x"`
	}

	hash := common.HexToHash("0xcafe")
	cfg := &params.ChainConfig{ChainID: big.NewInt(123)}
	WriteChainConfig(db, hash, cfg, upgradeCfg{X: 7})

	var out upgradeCfg
	gotCfg := ReadChainConfig(db, hash, &out)
	require.NotNil(t, gotCfg)
	require.Equal(t, cfg.ChainID, gotCfg.ChainID)
	require.Equal(t, 7, out.X)
}

func TestChainConfigNilDoesNotWriteUpgrade(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	hash := common.HexToHash("0xadd")
	// Passing nil config should not write upgrade bytes
	WriteChainConfig(db, hash, nil, struct{}{})

	ok, err := db.Has(upgradeConfigKey(hash))
	require.NoError(t, err)
	require.False(t, ok)
}

func TestReadChainConfigInvalidUpgradeJSONReturnsNil(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	hash := common.HexToHash("0xbeef")
	// Write a valid base chain config
	rawdb.WriteChainConfig(db, hash, &params.ChainConfig{})
	// Write invalid upgrade JSON
	require.NoError(t, db.Put(upgradeConfigKey(hash), []byte("{")))

	var out struct{}
	got := ReadChainConfig(db, hash, &out)
	require.Nil(t, got)
}

func TestSyncPerformedLatestCases(t *testing.T) {
	cases := []struct {
		name   string
		writes []uint64
		want   uint64
	}{
		{name: "empty", writes: nil, want: 0},
		{name: "increasing", writes: []uint64{1, 2, 3}, want: 3},
		{name: "unsorted", writes: []uint64{10, 5, 7}, want: 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			for _, n := range tc.writes {
				require.NoError(t, WriteSyncPerformed(db, n))
			}
			require.Equal(t, tc.want, GetLatestSyncPerformed(db))
		})
	}
}

func TestSyncSegmentsByRootTable(t *testing.T) {
	type entry struct {
		root   common.Hash
		starts []common.Hash
	}
	entries := []entry{
		{root: common.HexToHash("0xaaa"), starts: []common.Hash{common.HexToHash("0x1"), common.HexToHash("0x2")}},
		{root: common.HexToHash("0xbbb"), starts: []common.Hash{common.HexToHash("0x3")}},
	}
	db := rawdb.NewMemoryDatabase()
	// seed
	for _, e := range entries {
		for _, s := range e.starts {
			require.NoError(t, WriteSyncSegment(db, e.root, s))
		}
	}
	for _, e := range entries {
		t.Run("segments_"+e.root.Hex(), func(t *testing.T) {
			it := NewSyncSegmentsIterator(db, e.root)
			defer it.Release()
			got := map[common.Hash]bool{}
			for it.Next() {
				_, start := UnpackSyncSegmentKey(it.Key())
				got[common.BytesToHash(start)] = true
			}
			for _, s := range e.starts {
				require.True(t, got[s])
			}
		})
	}
}

func TestSyncStorageTriesByRootTable(t *testing.T) {
	type entry struct {
		root     common.Hash
		accounts []common.Hash
	}
	entries := []entry{
		{root: common.HexToHash("0xabc"), accounts: []common.Hash{common.HexToHash("0x1"), common.HexToHash("0x2")}},
		{root: common.HexToHash("0xdef"), accounts: []common.Hash{common.HexToHash("0x3")}},
	}
	db := rawdb.NewMemoryDatabase()
	// seed
	for _, e := range entries {
		for _, a := range e.accounts {
			require.NoError(t, WriteSyncStorageTrie(db, e.root, a))
		}
	}
	for _, e := range entries {
		t.Run("storage_"+e.root.Hex(), func(t *testing.T) {
			it := NewSyncStorageTriesIterator(db, nil)
			defer it.Release()
			got := map[common.Hash]bool{}
			for it.Next() {
				r, a := UnpackSyncStorageTrieKey(it.Key())
				if r == e.root {
					got[a] = true
				}
			}
			for _, a := range e.accounts {
				require.True(t, got[a])
			}
		})
	}
}

func TestCodeToFetchCases(t *testing.T) {
	cases := []struct {
		name   string
		hashes []common.Hash
		del    *common.Hash
		want   int
	}{
		{
			name:   "none",
			hashes: nil,
			del:    nil,
			want:   0,
		},
		{
			name:   "three_keep",
			hashes: []common.Hash{common.HexToHash("0x1"), common.HexToHash("0x2"), common.HexToHash("0x3")},
			del:    nil,
			want:   3,
		},
		{
			name:   "three_delete_one",
			hashes: []common.Hash{common.HexToHash("0x1"), common.HexToHash("0x2"), common.HexToHash("0x3")},
			del:    func() *common.Hash { h := common.HexToHash("0x2"); return &h }(),
			want:   2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			for _, h := range tc.hashes {
				WriteCodeToFetch(db, h)
			}
			if tc.del != nil {
				DeleteCodeToFetch(db, *tc.del)
			}
			it := NewCodeToFetchIterator(db)
			defer it.Release()
			count := 0
			for it.Next() {
				count++
			}
			require.Equal(t, tc.want, count)
		})
	}
}

func TestChainConfigCases(t *testing.T) {
	type upgrade struct {
		X int `json:"x"`
	}
	cases := []struct {
		name       string
		cfg        *params.ChainConfig
		up         any
		mutate     func(db ethdb.KeyValueStore, h common.Hash)
		wantCfgNil bool
		wantUp     *upgrade
	}{
		{
			name:       "valid-upgrade",
			cfg:        &params.ChainConfig{ChainID: big.NewInt(1)},
			up:         upgrade{X: 7},
			mutate:     nil,
			wantCfgNil: false,
			wantUp:     &upgrade{X: 7},
		},
		{
			name:       "nil-config",
			cfg:        nil,
			up:         struct{}{},
			mutate:     nil,
			wantCfgNil: true,
			wantUp:     nil,
		},
		{
			name:       "invalid-upgrade-json",
			cfg:        &params.ChainConfig{ChainID: big.NewInt(2)},
			up:         upgrade{X: 1},
			mutate:     func(db ethdb.KeyValueStore, h common.Hash) { _ = db.Put(upgradeConfigKey(h), []byte("{")) },
			wantCfgNil: true,
			wantUp:     nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			h := common.HexToHash("0x100")
			WriteChainConfig(db, h, tc.cfg, upgrade{X: 0})
			if tc.mutate != nil {
				tc.mutate(db, h)
			} else if tc.up != nil && tc.cfg != nil {
				// If provided, overwrite with provided upgrade object
				WriteChainConfig(db, h, tc.cfg, tc.up.(upgrade))
			}
			var out upgrade
			got := ReadChainConfig(db, h, &out)
			if tc.wantCfgNil {
				require.Nil(t, got)
			} else {
				require.NotNil(t, got)
			}
			if tc.wantUp != nil {
				require.Equal(t, *tc.wantUp, out)
			}
		})
	}
}
