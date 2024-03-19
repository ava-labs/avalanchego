// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interval

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
)

func TestGetMissingBlockIDs(t *testing.T) {
	blocks := generateBlockchain(7)
	parser := makeParser(blocks)

	db := memdb.New()
	tree, err := NewTree(db)
	require.NoError(t, err)
	lastAcceptedHeight := uint64(1)

	t.Run("initially empty", func(t *testing.T) {
		require := require.New(t)

		missing, err := GetMissingBlockIDs(
			context.Background(),
			db,
			parser,
			tree,
			lastAcceptedHeight,
		)
		require.NoError(err)
		require.Empty(missing)
	})

	t.Run("adding first block", func(t *testing.T) {
		require := require.New(t)

		_, err := Add(
			db,
			tree,
			lastAcceptedHeight,
			blocks[5],
		)
		require.NoError(err)

		missing, err := GetMissingBlockIDs(
			context.Background(),
			db,
			parser,
			tree,
			lastAcceptedHeight,
		)
		require.NoError(err)
		require.Equal(
			set.Of(
				blocks[4].ID(),
			),
			missing,
		)
	})

	t.Run("adding second block", func(t *testing.T) {
		require := require.New(t)

		_, err := Add(
			db,
			tree,
			lastAcceptedHeight,
			blocks[3],
		)
		require.NoError(err)

		missing, err := GetMissingBlockIDs(
			context.Background(),
			db,
			parser,
			tree,
			lastAcceptedHeight,
		)
		require.NoError(err)
		require.Equal(
			set.Of(
				blocks[2].ID(),
				blocks[4].ID(),
			),
			missing,
		)
	})

	t.Run("adding last desired block", func(t *testing.T) {
		require := require.New(t)

		_, err := Add(
			db,
			tree,
			lastAcceptedHeight,
			blocks[2],
		)
		require.NoError(err)

		missing, err := GetMissingBlockIDs(
			context.Background(),
			db,
			parser,
			tree,
			lastAcceptedHeight,
		)
		require.NoError(err)
		require.Equal(
			set.Of(
				blocks[4].ID(),
			),
			missing,
		)
	})

	t.Run("adding block with known parent", func(t *testing.T) {
		require := require.New(t)

		_, err := Add(
			db,
			tree,
			lastAcceptedHeight,
			blocks[6],
		)
		require.NoError(err)

		missing, err := GetMissingBlockIDs(
			context.Background(),
			db,
			parser,
			tree,
			lastAcceptedHeight,
		)
		require.NoError(err)
		require.Equal(
			set.Of(
				blocks[4].ID(),
			),
			missing,
		)
	})
}

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

func TestExecute(t *testing.T) {
	require := require.New(t)

	const numBlocks = 2*max(batchWritePeriod, iteratorReleasePeriod) + 1
	blocks := generateBlockchain(numBlocks)
	parser := makeParser(blocks)

	db := memdb.New()
	tree, err := NewTree(db)
	require.NoError(err)
	const lastAcceptedHeight = 1

	for i, block := range blocks[lastAcceptedHeight+1:] {
		newlyWantsParent, err := Add(
			db,
			tree,
			lastAcceptedHeight,
			block,
		)
		require.NoError(err)
		require.False(newlyWantsParent)
		require.Equal(uint64(i+1), tree.Len())
	}

	err = Execute(
		context.Background(),
		logging.NoLog{},
		db,
		parser,
		tree,
		lastAcceptedHeight,
	)
	require.NoError(err)

	for _, block := range blocks[lastAcceptedHeight+1:] {
		require.Equal(choices.Accepted, block.Status())
	}

	size, err := database.Count(db)
	require.NoError(err)
	require.Zero(size)
}

func TestExecuteExitsWhenCancelled(t *testing.T) {
	require := require.New(t)

	blocks := generateBlockchain(7)
	parser := makeParser(blocks)

	db := memdb.New()
	tree, err := NewTree(db)
	require.NoError(err)
	const lastAcceptedHeight = 1

	for i, block := range blocks[lastAcceptedHeight+1:] {
		newlyWantsParent, err := Add(
			db,
			tree,
			lastAcceptedHeight,
			block,
		)
		require.NoError(err)
		require.False(newlyWantsParent)
		require.Equal(uint64(i+1), tree.Len())
	}

	startSize, err := database.Count(db)
	require.NoError(err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = Execute(
		ctx,
		logging.NoLog{},
		db,
		parser,
		tree,
		lastAcceptedHeight,
	)
	require.ErrorIs(err, context.Canceled)

	for _, block := range blocks[lastAcceptedHeight+1:] {
		require.Equal(choices.Processing, block.Status())
	}

	endSize, err := database.Count(db)
	require.NoError(err)
	require.Equal(startSize, endSize)
}

func TestExecuteSkipsAcceptedBlocks(t *testing.T) {
	require := require.New(t)

	blocks := generateBlockchain(7)
	parser := makeParser(blocks)

	db := memdb.New()
	tree, err := NewTree(db)
	require.NoError(err)
	const (
		lastAcceptedHeightWhenAdding    = 1
		lastAcceptedHeightWhenExecuting = 3
	)

	for i, block := range blocks[lastAcceptedHeightWhenAdding+1:] {
		newlyWantsParent, err := Add(
			db,
			tree,
			lastAcceptedHeightWhenAdding,
			block,
		)
		require.NoError(err)
		require.False(newlyWantsParent)
		require.Equal(uint64(i+1), tree.Len())
	}

	err = Execute(
		context.Background(),
		logging.NoLog{},
		db,
		parser,
		tree,
		lastAcceptedHeightWhenExecuting,
	)
	require.NoError(err)

	for _, block := range blocks[lastAcceptedHeightWhenAdding+1 : lastAcceptedHeightWhenExecuting] {
		require.Equal(choices.Processing, block.Status())
	}
	for _, block := range blocks[lastAcceptedHeightWhenExecuting+1:] {
		require.Equal(choices.Accepted, block.Status())
	}

	size, err := database.Count(db)
	require.NoError(err)
	require.Zero(size)
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

type testParser func(context.Context, []byte) (snowman.Block, error)

func (f testParser) ParseBlock(ctx context.Context, bytes []byte) (snowman.Block, error) {
	return f(ctx, bytes)
}

func makeParser(blocks []snowman.Block) Parser {
	return testParser(func(_ context.Context, b []byte) (snowman.Block, error) {
		for _, block := range blocks {
			if bytes.Equal(b, block.Bytes()) {
				return block, nil
			}
		}
		return nil, database.ErrNotFound
	})
}
