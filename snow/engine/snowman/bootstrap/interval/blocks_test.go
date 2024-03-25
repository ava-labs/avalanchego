// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interval

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
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
			blocks[5],
		)
		require.NoError(err)
		require.True(newlyWantsParent)
		require.Equal(uint64(1), tree.Len())

		bytes, err := GetBlock(db, blocks[5].Height())
		require.NoError(err)
		require.Equal(blocks[5].Bytes(), bytes)
	})

	t.Run("adding duplicate block", func(t *testing.T) {
		require := require.New(t)

		newlyWantsParent, err := Add(
			db,
			tree,
			lastAcceptedHeight,
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
			blocks[3],
		)
		require.NoError(err)
		require.True(newlyWantsParent)
		require.Equal(uint64(2), tree.Len())

		bytes, err := GetBlock(db, blocks[3].Height())
		require.NoError(err)
		require.Equal(blocks[3].Bytes(), bytes)
	})

	t.Run("adding last desired block", func(t *testing.T) {
		require := require.New(t)

		newlyWantsParent, err := Add(
			db,
			tree,
			lastAcceptedHeight,
			blocks[2],
		)
		require.NoError(err)
		require.False(newlyWantsParent)
		require.Equal(uint64(3), tree.Len())

		bytes, err := GetBlock(db, blocks[2].Height())
		require.NoError(err)
		require.Equal(blocks[2].Bytes(), bytes)
	})

	t.Run("adding undesired block", func(t *testing.T) {
		require := require.New(t)

		newlyWantsParent, err := Add(
			db,
			tree,
			lastAcceptedHeight,
			blocks[1],
		)
		require.NoError(err)
		require.False(newlyWantsParent)
		require.Equal(uint64(3), tree.Len())

		_, err = GetBlock(db, blocks[1].Height())
		require.ErrorIs(err, database.ErrNotFound)
	})

	t.Run("adding block with known parent", func(t *testing.T) {
		require := require.New(t)

		newlyWantsParent, err := Add(
			db,
			tree,
			lastAcceptedHeight,
			blocks[6],
		)
		require.NoError(err)
		require.False(newlyWantsParent)
		require.Equal(uint64(4), tree.Len())

		bytes, err := GetBlock(db, blocks[6].Height())
		require.NoError(err)
		require.Equal(blocks[6].Bytes(), bytes)
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
			blocks[5],
		)
		require.NoError(err)
		require.True(newlyWantsParent)
		require.Equal(uint64(1), tree.Len())

		bytes, err := GetBlock(db, blocks[5].Height())
		require.NoError(err)
		require.Equal(blocks[5].Bytes(), bytes)
	})

	t.Run("removing a block", func(t *testing.T) {
		require := require.New(t)

		err := Remove(
			db,
			tree,
			blocks[5].Height(),
		)
		require.NoError(err)
		require.Zero(tree.Len())

		_, err = GetBlock(db, blocks[5].Height())
		require.ErrorIs(err, database.ErrNotFound)
	})
}

func generateBlockchain(length uint64) []snowman.Block {
	if length == 0 {
		return nil
	}

	blocks := make([]snowman.Block, length)
	blocks[0] = &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: ids.Empty,
		HeightV: 0,
		BytesV:  utils.RandomBytes(1024),
	}
	for height := uint64(1); height < length; height++ {
		blocks[height] = &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Processing,
			},
			ParentV: blocks[height-1].ID(),
			HeightV: height,
			BytesV:  utils.RandomBytes(1024),
		}
	}
	return blocks
}
