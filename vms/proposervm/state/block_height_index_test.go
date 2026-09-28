// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
)

func testHeightIndex(require *require.Assertions, hi HeightIndex) {
	_, err := hi.GetMinimumHeight()
	require.Equal(database.ErrNotFound, err)

	// An empty index reports every requested height as missing.
	blkIDs, err := hi.GetBlockIDsAtHeights(0, 2)
	require.NoError(err)
	require.Equal([]ids.ID{ids.Empty, ids.Empty, ids.Empty}, blkIDs)

	var (
		minIndexedHeight uint64 = 3
		maxIndexedHeight uint64 = 6
		indexedIDs              = make(map[uint64]ids.ID)
	)
	for height := minIndexedHeight; height <= maxIndexedHeight; height++ {
		blkID := ids.GenerateTestID()
		indexedIDs[height] = blkID
		require.NoError(hi.SetBlockIDAtHeight(height, blkID))
	}

	minHeight, err := hi.GetMinimumHeight()
	require.NoError(err)
	require.Equal(minIndexedHeight, minHeight)

	for height, blkID := range indexedIDs {
		fetchedID, err := hi.GetBlockIDAtHeight(height)
		require.NoError(err)
		require.Equal(blkID, fetchedID)
	}

	// Heights outside of the indexed range are reported as missing while the
	// indexed heights are returned in ascending order.
	blkIDs, err = hi.GetBlockIDsAtHeights(minIndexedHeight-2, maxIndexedHeight+2)
	require.NoError(err)
	require.Equal([]ids.ID{
		ids.Empty,
		ids.Empty,
		indexedIDs[3],
		indexedIDs[4],
		indexedIDs[5],
		indexedIDs[6],
		ids.Empty,
		ids.Empty,
	}, blkIDs)

	// A range within the indexed heights only returns those heights.
	blkIDs, err = hi.GetBlockIDsAtHeights(4, 5)
	require.NoError(err)
	require.Equal([]ids.ID{indexedIDs[4], indexedIDs[5]}, blkIDs)

	// A single height range works.
	blkIDs, err = hi.GetBlockIDsAtHeights(6, 6)
	require.NoError(err)
	require.Equal([]ids.ID{indexedIDs[6]}, blkIDs)

	// An inverted range is empty.
	blkIDs, err = hi.GetBlockIDsAtHeights(6, 5)
	require.NoError(err)
	require.Empty(blkIDs)

	// Deleting a height leaves a gap in the range.
	require.NoError(hi.DeleteBlockIDAtHeight(4))
	blkIDs, err = hi.GetBlockIDsAtHeights(3, 5)
	require.NoError(err)
	require.Equal([]ids.ID{indexedIDs[3], ids.Empty, indexedIDs[5]}, blkIDs)

	_, err = hi.GetBlockIDAtHeight(4)
	require.Equal(database.ErrNotFound, err)

	_, err = hi.GetForkHeight()
	require.Equal(database.ErrNotFound, err)

	require.NoError(hi.SetForkHeight(minIndexedHeight))
	forkHeight, err := hi.GetForkHeight()
	require.NoError(err)
	require.Equal(minIndexedHeight, forkHeight)
}

func TestHeightIndex(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	vdb := versiondb.New(db)
	hi := NewHeightIndex(vdb, vdb)

	testHeightIndex(require, hi)
}

// TestHeightIndexUncommittedWrites verifies that the range read observes
// heights that were written but not yet committed to the underlying database.
func TestHeightIndexUncommittedWrites(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	vdb := versiondb.New(db)
	hi := NewHeightIndex(vdb, vdb)

	committedID := ids.GenerateTestID()
	require.NoError(hi.SetBlockIDAtHeight(1, committedID))
	require.NoError(vdb.Commit())

	uncommittedID := ids.GenerateTestID()
	require.NoError(hi.SetBlockIDAtHeight(2, uncommittedID))

	blkIDs, err := hi.GetBlockIDsAtHeights(1, 2)
	require.NoError(err)
	require.Equal([]ids.ID{committedID, uncommittedID}, blkIDs)
}
