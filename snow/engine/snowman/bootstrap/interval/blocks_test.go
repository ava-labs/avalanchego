// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interval

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/utils"
)

func TestAdd(t *testing.T) {
	blocks := generateBlockchain(7)

	db := memdb.New()
	tree, err := NewTree(db)
	require.NoError(t, err)
	lastAcceptedHeight := uint64(1)

	t.Run("adding first block", func(t *testing.T) {
		require := require.New(t)

		newlyWantsParent, err := Add(
			db,
			tree,
			lastAcceptedHeight,
			5,
			blocks[5],
		)
		require.NoError(err)
		require.True(newlyWantsParent)
		require.Equal(uint64(1), tree.Len())

		bytes, err := GetBlock(db, 5)
		require.NoError(err)
		require.Equal(blocks[5], bytes)
	})

	t.Run("adding duplicate block", func(t *testing.T) {
		require := require.New(t)

		newlyWantsParent, err := Add(
			db,
			tree,
			lastAcceptedHeight,
			5,
			blocks[5],
		)
		require.NoError(err)
		require.False(newlyWantsParent)
		require.Equal(uint64(1), tree.Len())
	})

	t.Run("adding second block", func(t *testing.T) {
		require := require.New(t)

		newlyWantsParent, err := Add(
			db,
			tree,
			lastAcceptedHeight,
			3,
			blocks[3],
		)
		require.NoError(err)
		require.True(newlyWantsParent)
		require.Equal(uint64(2), tree.Len())

		bytes, err := GetBlock(db, 3)
		require.NoError(err)
		require.Equal(blocks[3], bytes)
	})

	t.Run("adding last desired block", func(t *testing.T) {
		require := require.New(t)

		newlyWantsParent, err := Add(
			db,
			tree,
			lastAcceptedHeight,
			2,
			blocks[2],
		)
		require.NoError(err)
		require.False(newlyWantsParent)
		require.Equal(uint64(3), tree.Len())

		bytes, err := GetBlock(db, 2)
		require.NoError(err)
		require.Equal(blocks[2], bytes)
	})

	t.Run("adding undesired block", func(t *testing.T) {
		require := require.New(t)

		newlyWantsParent, err := Add(
			db,
			tree,
			lastAcceptedHeight,
			1,
			blocks[1],
		)
		require.NoError(err)
		require.False(newlyWantsParent)
		require.Equal(uint64(3), tree.Len())

		_, err = GetBlock(db, 1)
		require.ErrorIs(err, database.ErrNotFound)
	})

	t.Run("adding block with known parent", func(t *testing.T) {
		require := require.New(t)

		newlyWantsParent, err := Add(
			db,
			tree,
			lastAcceptedHeight,
			6,
			blocks[6],
		)
		require.NoError(err)
		require.False(newlyWantsParent)
		require.Equal(uint64(4), tree.Len())

		bytes, err := GetBlock(db, 6)
		require.NoError(err)
		require.Equal(blocks[6], bytes)
	})
}

func TestRemove(t *testing.T) {
	blocks := generateBlockchain(7)

	db := memdb.New()
	tree, err := NewTree(db)
	require.NoError(t, err)
	lastAcceptedHeight := uint64(1)

	t.Run("adding a block", func(t *testing.T) {
		require := require.New(t)

		newlyWantsParent, err := Add(
			db,
			tree,
			lastAcceptedHeight,
			5,
			blocks[5],
		)
		require.NoError(err)
		require.True(newlyWantsParent)
		require.Equal(uint64(1), tree.Len())

		bytes, err := GetBlock(db, 5)
		require.NoError(err)
		require.Equal(blocks[5], bytes)
	})

	t.Run("removing a block", func(t *testing.T) {
		require := require.New(t)

		require.NoError(Remove(
			db,
			tree,
			5,
		))
		require.Zero(tree.Len())

		_, err = GetBlock(db, 5)
		require.ErrorIs(err, database.ErrNotFound)
	})
}

func generateBlockchain(length uint64) [][]byte {
	blocks := make([][]byte, length)
	for i := range blocks {
		blocks[i] = utils.RandomBytes(1024)
	}
	return blocks
}
