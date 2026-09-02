// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/stretchr/testify/require"
)

// fillHash returns a hash with every byte set to b, giving distinct,
// trivially ordered keys.
func fillHash(b byte) common.Hash {
	var h common.Hash
	for i := range h {
		h[i] = b
	}
	return h
}

func TestSyncSnapAccountIterator(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	accounts := map[common.Hash][]byte{
		fillHash(0x11): []byte("account-11"),
		fillHash(0x22): []byte("account-22"),
		fillHash(0x33): []byte("account-33"),
	}
	for hash, val := range accounts {
		rawdb.WriteAccountSnapshot(db, hash, val)
	}
	// Same-prefix keys of a different length (e.g. trie nodes sharing the
	// single-byte snapshot prefix) MUST be skipped.
	shortKey := append(slices.Clone(rawdb.SnapshotAccountPrefix), 0x11, 0x11)
	require.NoError(t, db.Put(shortKey, []byte("not-an-account")), "Put() short same-prefix key")
	// Storage entries live under another prefix and MUST NOT surface.
	rawdb.WriteStorageSnapshot(db, fillHash(0x11), fillHash(0xaa), []byte("slot"))

	tests := []struct {
		name       string
		start      common.Hash
		wantHashes []common.Hash
	}{
		{
			name:       "from zero",
			start:      common.Hash{},
			wantHashes: []common.Hash{fillHash(0x11), fillHash(0x22), fillHash(0x33)},
		},
		{
			name:       "from existing key",
			start:      fillHash(0x22),
			wantHashes: []common.Hash{fillHash(0x22), fillHash(0x33)},
		},
		{
			name:       "from between keys",
			start:      fillHash(0x23),
			wantHashes: []common.Hash{fillHash(0x33)},
		},
		{
			name:       "past last key",
			start:      fillHash(0x44),
			wantHashes: nil,
		},
	}

	snap := &syncSnap{db: db}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			it, err := snap.AccountIterator(tt.start)
			require.NoError(t, err, "syncSnap.AccountIterator(%s)", tt.start)
			defer it.Release()

			var gotHashes []common.Hash
			for it.Next() {
				gotHashes = append(gotHashes, it.Hash())
				require.Equal(t, accounts[it.Hash()], it.Account(), "syncSnap.AccountIterator(%s) value at %s", tt.start, it.Hash())
			}
			require.NoError(t, it.Error(), "iterator error after AccountIterator(%s)", tt.start)
			require.Equal(t, tt.wantHashes, gotHashes, "hashes from AccountIterator(%s)", tt.start)
		})
	}
}

func TestSyncSnapStorageIterator(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	account := fillHash(0x11)
	slots := map[common.Hash][]byte{
		fillHash(0xaa): []byte("slot-aa"),
		fillHash(0xbb): []byte("slot-bb"),
	}
	for hash, val := range slots {
		rawdb.WriteStorageSnapshot(db, account, hash, val)
	}
	// Another account's slots and the account entries themselves MUST NOT
	// surface.
	rawdb.WriteStorageSnapshot(db, fillHash(0x22), fillHash(0xaa), []byte("other-account-slot"))
	rawdb.WriteAccountSnapshot(db, account, []byte("account-11"))

	tests := []struct {
		name       string
		start      common.Hash
		wantHashes []common.Hash
	}{
		{
			name:       "from zero",
			start:      common.Hash{},
			wantHashes: []common.Hash{fillHash(0xaa), fillHash(0xbb)},
		},
		{
			name:       "from between keys",
			start:      fillHash(0xab),
			wantHashes: []common.Hash{fillHash(0xbb)},
		},
	}

	snap := &syncSnap{db: db}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			it, err := snap.StorageIterator(account, tt.start)
			require.NoError(t, err, "syncSnap.StorageIterator(%s, %s)", account, tt.start)
			defer it.Release()

			var gotHashes []common.Hash
			for it.Next() {
				gotHashes = append(gotHashes, it.Hash())
				require.Equal(t, slots[it.Hash()], it.Slot(), "syncSnap.StorageIterator(%s) value at %s", tt.start, it.Hash())
			}
			require.NoError(t, it.Error(), "iterator error after StorageIterator(%s)", tt.start)
			require.Equal(t, tt.wantHashes, gotHashes, "hashes from StorageIterator(%s)", tt.start)
		})
	}
}

// TestDiskIteratorRetainsValues asserts the [hashdb.Snapshot] contract that
// returned bytes survive later calls to Next and Release.
func TestDiskIteratorRetainsValues(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	rawdb.WriteAccountSnapshot(db, fillHash(0x11), []byte("account-11"))
	rawdb.WriteAccountSnapshot(db, fillHash(0x22), []byte("account-22"))

	snap := &syncSnap{db: db}
	it, err := snap.AccountIterator(common.Hash{})
	require.NoError(t, err, "syncSnap.AccountIterator()")
	defer it.Release()

	require.True(t, it.Next(), "AccountIterator.Next() first entry")
	firstHash, firstVal := it.Hash(), it.Account()
	require.True(t, it.Next(), "AccountIterator.Next() second entry")
	it.Release()

	require.Equal(t, fillHash(0x11), firstHash, "first hash after Next and Release")
	require.Equal(t, []byte("account-11"), firstVal, "first value after Next and Release")
}
