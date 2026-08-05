// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
)

// Stale markers are wiped on a root mismatch, kept when it matches.
func TestTrieQueue_ClearIfRootDoesNotMatch(t *testing.T) {
	t.Parallel()
	const target = "0xbeef"
	segmentTrie := common.HexToHash("0x33")

	tests := []struct {
		name       string
		storedRoot string
		wantKept   bool
	}{
		{
			name:       "same root keeps markers",
			storedRoot: target,
			wantKept:   true,
		},
		{
			name:       "root mismatch wipes markers",
			storedRoot: "0xdead",
			wantKept:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := rawdb.NewMemoryDatabase()
			require.NoError(t, customrawdb.WriteSyncRoot(db, common.HexToHash(tt.storedRoot)))
			require.NoError(t, customrawdb.WriteSyncStorageTrie(db, common.HexToHash("0x11"), common.HexToHash("0x22")))
			require.NoError(t, customrawdb.WriteSyncSegment(db, segmentTrie, common.Hash{}))

			require.NoError(t, newTrieQueue(db).clearIfRootDoesNotMatch(common.HexToHash(target)))

			got, err := customrawdb.ReadSyncRoot(db)
			require.NoError(t, err)
			require.Equal(t, common.HexToHash(target), got, "the target root must be recorded")

			stIt := customrawdb.NewSyncStorageTriesIterator(db, nil)
			defer stIt.Release()
			require.Equal(t, tt.wantKept, stIt.Next(), "storage-trie markers")

			segIt := customrawdb.NewSyncSegmentsIterator(db, segmentTrie)
			defer segIt.Release()
			require.Equal(t, tt.wantKept, segIt.Next(), "segment markers")
		})
	}
}

// Accounts sharing a root must arrive grouped under it.
func TestTrieQueue_StorageTries(t *testing.T) {
	t.Parallel()

	var (
		rootA = common.HexToHash("0x0a")
		rootB = common.HexToHash("0x0b")
		acct1 = common.HexToHash("0x01")
		acct2 = common.HexToHash("0x02")
	)

	tests := []struct {
		name  string
		write map[common.Hash][]common.Hash
		want  []storageTrieRef
	}{
		{
			name: "empty queue",
		},
		{
			name:  "one trie, one account",
			write: map[common.Hash][]common.Hash{rootA: {acct1}},
			want:  []storageTrieRef{{root: rootA, accounts: []common.Hash{acct1}}},
		},
		{
			name:  "accounts sharing a root are grouped",
			write: map[common.Hash][]common.Hash{rootA: {acct1, acct2}},
			want:  []storageTrieRef{{root: rootA, accounts: []common.Hash{acct1, acct2}}},
		},
		{
			name:  "two tries come back in root order",
			write: map[common.Hash][]common.Hash{rootA: {acct1}, rootB: {acct2}},
			want: []storageTrieRef{
				{root: rootA, accounts: []common.Hash{acct1}},
				{root: rootB, accounts: []common.Hash{acct2}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := rawdb.NewMemoryDatabase()
			for root, accounts := range tt.write {
				for _, account := range accounts {
					require.NoError(t, customrawdb.WriteSyncStorageTrie(db, root, account))
				}
			}

			var got []storageTrieRef
			for ref, err := range newTrieQueue(db).storageTries() {
				require.NoError(t, err)
				got = append(got, ref)
			}

			require.Len(t, got, len(tt.want))
			for i, want := range tt.want {
				require.Equal(t, want.root, got[i].root)
				require.ElementsMatch(t, want.accounts, got[i].accounts)
			}
		})
	}
}

// An early break must stop the scan.
func TestTrieQueue_StorageTriesBreak(t *testing.T) {
	t.Parallel()

	db := rawdb.NewMemoryDatabase()
	for i := range 5 {
		require.NoError(t, customrawdb.WriteSyncStorageTrie(db, common.BigToHash(big.NewInt(int64(i+1))), common.HexToHash("0x01")))
	}

	var seen int
	for range newTrieQueue(db).storageTries() {
		seen++
		break
	}
	require.Equal(t, 1, seen, "break must end the iteration")
}
