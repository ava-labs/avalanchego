// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
)

// TestTrieQueue_ClearIfRootDoesNotMatch checks stale markers are wiped on a root mismatch and kept when the root matches.
func TestTrieQueue_ClearIfRootDoesNotMatch(t *testing.T) {
	const target = "0xbeef"
	segmentTrie := common.HexToHash("0x33")

	tests := []struct {
		name       string
		storedRoot string
		wantKept   bool
	}{
		{name: "same root keeps markers", storedRoot: target, wantKept: true},
		{name: "root mismatch wipes markers", storedRoot: "0xdead", wantKept: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
