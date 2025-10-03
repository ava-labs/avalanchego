// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customrawdb

import (
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/stretchr/testify/require"
)

func TestClearPrefix(t *testing.T) {
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
