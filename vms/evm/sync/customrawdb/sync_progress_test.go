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

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/set"
)

func TestClearAllSyncSegments(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	// add a key that should be cleared
	require.NoError(t, WriteSyncSegment(db, common.Hash{1}, common.Hash{}))

	// add a key that should not be cleared
	key := slices.Concat(syncSegmentsPrefix, []byte("foo"))
	require.NoError(t, db.Put(key, []byte("bar")))

	require.NoError(t, ClearAllSyncSegments(db))

	// No well-formed segment keys should remain.
	iter := rawdb.NewKeyLengthIterator(db.NewIterator(syncSegmentsPrefix, nil), syncSegmentsKeyLength)
	keys := mapIterator(t, iter, common.CopyBytes)
	require.Empty(t, keys)
	// The malformed key should still be present.
	has, err := db.Has(key)
	require.NoError(t, err)
	require.True(t, has)
}

func TestWriteReadSyncRoot(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	// No root written yet
	root, err := ReadSyncRoot(db)
	require.ErrorIs(t, err, database.ErrNotFound)
	require.Zero(t, root)

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

	require.NoError(t, WriteCodeToFetch(db, h1))
	require.NoError(t, WriteCodeToFetch(db, h2))

	// Insert a malformed key that should be ignored by the iterator (wrong length)
	bad := slices.Concat(CodeToFetchPrefix, h1.Bytes(), []byte{0x00})
	require.NoError(t, db.Put(bad, []byte("x")))

	// Collect hashes from iterator and assert presence.
	vals := mapIterator(t, NewCodeToFetchIterator(db), func(key []byte) common.Hash {
		return common.BytesToHash(key[len(CodeToFetchPrefix):])
	})

	seen := set.Of(vals...)
	require.Contains(t, seen, h1)
	require.Contains(t, seen, h2)

	// Delete one and confirm only one remains
	require.NoError(t, DeleteCodeToFetch(db, h1))
	iter := rawdb.NewKeyLengthIterator(db.NewIterator(CodeToFetchPrefix, nil), codeToFetchKeyLength)
	keys := mapIterator(t, iter, common.CopyBytes)
	require.Len(t, keys, 1)
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

	// Iterate only over rootA and assert exact keys.
	keys := mapIterator(t, NewSyncSegmentsIterator(db, rootA), common.CopyBytes)
	expectedA := [][]byte{buildSegmentKey(rootA, start1), buildSegmentKey(rootA, start2)}
	require.Equal(t, expectedA, keys)

	// Clear only rootA.
	require.NoError(t, ClearSyncSegments(db, rootA))
	keys = mapIterator(t, NewSyncSegmentsIterator(db, rootA), common.CopyBytes)
	require.Empty(t, keys)

	// RootB remains.
	keys = mapIterator(t, NewSyncSegmentsIterator(db, rootB), common.CopyBytes)
	expectedB := [][]byte{buildSegmentKey(rootB, start3)}
	require.Equal(t, expectedB, keys)
}

func TestStorageTriesIteratorUnpackAndClear(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	root := common.HexToHash("0xabc")
	acct1 := common.HexToHash("0x01")
	acct2 := common.HexToHash("0x02")

	require.NoError(t, WriteSyncStorageTrie(db, root, acct1))
	require.NoError(t, WriteSyncStorageTrie(db, root, acct2))

	keys := mapIterator(t, NewSyncStorageTriesIterator(db, nil), common.CopyBytes)
	expected := [][]byte{buildStorageTrieKey(root, acct1), buildStorageTrieKey(root, acct2)}
	require.Equal(t, expected, keys)

	require.NoError(t, ClearSyncStorageTrie(db, root))
	keys = mapIterator(t, NewSyncStorageTriesIterator(db, nil), common.CopyBytes)
	require.Empty(t, keys)
}

func TestClearAllSyncStorageTries(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	root := common.HexToHash("0xabc")
	// Keys that should be cleared
	require.NoError(t, WriteSyncStorageTrie(db, root, common.HexToHash("0x01")))
	require.NoError(t, WriteSyncStorageTrie(db, root, common.HexToHash("0x02")))
	// Key that should not be cleared due to wrong length.
	bad := slices.Concat(
		syncStorageTriesPrefix,
		root.Bytes(),
		common.HexToHash("0xff").Bytes(),
		[]byte{0x00},
	)
	require.NoError(t, db.Put(bad, []byte("x")))

	require.NoError(t, ClearAllSyncStorageTries(db))

	// No well-formed storage trie keys should remain.
	iter := rawdb.NewKeyLengthIterator(db.NewIterator(syncStorageTriesPrefix, nil), syncStorageTriesKeyLength)
	keys := mapIterator(t, iter, common.CopyBytes)
	require.Empty(t, keys)
	// The malformed key should still be present.
	has, err := db.Has(bad)
	require.NoError(t, err)
	require.True(t, has)
}

func TestClearSyncSegments_NoKeys(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	root := common.HexToHash("0xabc")

	require.NoError(t, ClearSyncSegments(db, root))
	it := NewSyncSegmentsIterator(db, root)
	require.Empty(t, mapIterator(t, it, common.CopyBytes))
	require.NoError(t, it.Error())
}

func TestClearSyncStorageTrie_NoKeys(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	root := common.HexToHash("0xabc")

	require.NoError(t, ClearSyncStorageTrie(db, root))
	it := NewSyncStorageTriesIterator(db, nil)
	require.Empty(t, mapIterator(t, it, common.CopyBytes))
	require.NoError(t, it.Error())
}

func TestSyncPerformedAndLatest(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	require.NoError(t, WriteSyncPerformed(db, 10))
	require.NoError(t, WriteSyncPerformed(db, 20))
	require.NoError(t, WriteSyncPerformed(db, 15))

	// Iterator yields all
	vals := mapIterator(t, newSyncPerformedIterator(db), parseSyncPerformedKey)

	require.Equal(t, []uint64{10, 15, 20}, vals)

	// Latest is max
	latest, err := GetLatestSyncPerformed(db)
	require.NoError(t, err)
	require.Equal(t, uint64(20), latest)
}

func TestGetLatestSyncPerformedEmpty(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	latest, err := GetLatestSyncPerformed(db)
	require.NoError(t, err)
	require.Zero(t, latest)
}

func TestChainConfigReadWriteWithUpgrade(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	type upgradeCfg struct {
		X int `json:"x"`
	}

	hash := common.HexToHash("0xcafe")
	cfg := &params.ChainConfig{ChainID: big.NewInt(123)}
	require.NoError(t, WriteChainConfig(db, hash, cfg, upgradeCfg{X: 7}))

	var out upgradeCfg
	gotCfg, err := ReadChainConfig(db, hash, &out)
	require.NoError(t, err)
	require.NotNil(t, gotCfg)
	require.Equal(t, cfg.ChainID, gotCfg.ChainID)
	require.Equal(t, 7, out.X)
}

func TestChainConfigNilDoesNotWriteUpgrade(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	hash := common.HexToHash("0xadd")
	// Passing nil config should not write upgrade bytes
	require.NoError(t, WriteChainConfig(db, hash, nil, struct{}{}))

	ok, err := db.Has(upgradeConfigKey(hash))
	require.NoError(t, err)
	require.False(t, ok)
}

func TestSyncPerformedLatestCases(t *testing.T) {
	tests := []struct {
		name   string
		writes []uint64
		want   uint64
	}{
		{
			name: "empty",
			want: 0,
		},
		{
			name:   "increasing",
			writes: []uint64{1, 2, 3},
			want:   3,
		},
		{
			name:   "unsorted",
			writes: []uint64{10, 5, 7},
			want:   10,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			for _, n := range tc.writes {
				require.NoError(t, WriteSyncPerformed(db, n))
			}
			latest, err := GetLatestSyncPerformed(db)
			require.NoError(t, err)
			require.Equal(t, tc.want, latest)
		})
	}
}

func TestSyncSegmentsByRootTable(t *testing.T) {
	tests := []struct {
		name   string
		root   common.Hash
		starts []common.Hash
	}{
		{
			name:   "segments_multiple_starts",
			root:   common.HexToHash("0xaaa"),
			starts: []common.Hash{common.HexToHash("0x1"), common.HexToHash("0x2")},
		},
		{
			name:   "segments_single_start",
			root:   common.HexToHash("0xbbb"),
			starts: []common.Hash{common.HexToHash("0x3")},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			for _, s := range tc.starts {
				require.NoError(t, WriteSyncSegment(db, tc.root, s))
			}
			got := mapIterator(t, NewSyncSegmentsIterator(db, tc.root), func(k []byte) common.Hash {
				_, start := ParseSyncSegmentKey(k)
				return common.BytesToHash(start)
			})
			require.Equal(t, tc.starts, got)
		})
	}
}

func TestSyncStorageTriesByRootTable(t *testing.T) {
	tests := []struct {
		name     string
		root     common.Hash
		accounts []common.Hash
	}{
		{
			name:     "storage_multiple_accounts",
			root:     common.HexToHash("0xabc"),
			accounts: []common.Hash{common.HexToHash("0x1"), common.HexToHash("0x2")},
		},
		{
			name:     "storage_single_account",
			root:     common.HexToHash("0xdef"),
			accounts: []common.Hash{common.HexToHash("0x3")},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			for _, a := range tc.accounts {
				require.NoError(t, WriteSyncStorageTrie(db, tc.root, a))
			}
			got := mapIterator(t, NewSyncStorageTriesIterator(db, nil), func(k []byte) common.Hash {
				_, a := ParseSyncStorageTrieKey(k)
				return a
			})
			require.Equal(t, tc.accounts, got)
		})
	}
}

func TestCodeToFetchCases(t *testing.T) {
	h1 := common.HexToHash("0x1")
	h2 := common.HexToHash("0x2")
	h3 := common.HexToHash("0x3")

	tests := []struct {
		name   string
		hashes []common.Hash
		delIdx int // -1 => no delete
		want   int
	}{
		{
			name:   "none",
			delIdx: -1,
			want:   0,
		},
		{
			name:   "three_keep",
			hashes: []common.Hash{h1, h2, h3},
			delIdx: -1,
			want:   3,
		},
		{
			name:   "three_delete_one",
			hashes: []common.Hash{h1, h2, h3},
			delIdx: 1,
			want:   2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			for _, h := range tc.hashes {
				require.NoError(t, WriteCodeToFetch(db, h))
			}
			if tc.delIdx >= 0 {
				require.NoError(t, DeleteCodeToFetch(db, tc.hashes[tc.delIdx]))
			}
			iter := rawdb.NewKeyLengthIterator(db.NewIterator(CodeToFetchPrefix, nil), codeToFetchKeyLength)
			keys := mapIterator(t, iter, common.CopyBytes)
			require.Len(t, keys, tc.want)
		})
	}
}

func mapIterator[T any](t *testing.T, it ethdb.Iterator, f func([]byte) T) []T {
	t.Helper()
	defer it.Release()
	var out []T
	for it.Next() {
		out = append(out, f(it.Key()))
	}
	require.NoError(t, it.Error())
	return out
}

func buildSegmentKey(root, start common.Hash) []byte {
	return slices.Concat(syncSegmentsPrefix, root[:], start[:])
}

func buildStorageTrieKey(root, account common.Hash) []byte {
	return slices.Concat(syncStorageTriesPrefix, root[:], account[:])
}
