// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customrawdb

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	ethrawdb "github.com/ava-labs/libevm/core/rawdb"
	"github.com/stretchr/testify/require"
)

func TestClearPrefix(t *testing.T) {
	require := require.New(t)
	db := ethrawdb.NewMemoryDatabase()
	// add a key that should be cleared
	require.NoError(WriteSyncSegment(db, common.Hash{1}, common.Hash{}))

	// add a key that should not be cleared
	key := append(syncSegmentsPrefix, []byte("foo")...)
	require.NoError(db.Put(key, []byte("bar")))

	require.NoError(ClearAllSyncSegments(db))

	count := 0
	it := db.NewIterator(syncSegmentsPrefix, nil)
	defer it.Release()
	for it.Next() {
		count++
	}
	require.NoError(it.Error())
	require.Equal(1, count)
}
