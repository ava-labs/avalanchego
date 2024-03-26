// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

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
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/bootstrap/interval"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
)

func TestGetMissingBlockIDs(t *testing.T) {
	blocks := generateBlockchain(7)
	parser := makeParser(blocks)

	db := memdb.New()
	tree, err := interval.NewTree(db)
	require.NoError(t, err)
	lastAcceptedHeight := uint64(1)

	t.Run("initially empty", func(t *testing.T) {
		require := require.New(t)

		missing, err := getMissingBlockIDs(
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

		_, err := interval.Add(
			db,
			tree,
			lastAcceptedHeight,
			5,
			blocks[5].Bytes(),
		)
		require.NoError(err)

		missing, err := getMissingBlockIDs(
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

		_, err := interval.Add(
			db,
			tree,
			lastAcceptedHeight,
			3,
			blocks[3].Bytes(),
		)
		require.NoError(err)

		missing, err := getMissingBlockIDs(
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

		_, err := interval.Add(
			db,
			tree,
			lastAcceptedHeight,
			2,
			blocks[2].Bytes(),
		)
		require.NoError(err)

		missing, err := getMissingBlockIDs(
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

		_, err := interval.Add(
			db,
			tree,
			lastAcceptedHeight,
			6,
			blocks[6].Bytes(),
		)
		require.NoError(err)

		missing, err := getMissingBlockIDs(
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

func TestProcess(t *testing.T) {
	blocks := generateBlockchain(7)

	db := memdb.New()
	tree, err := interval.NewTree(db)
	require.NoError(t, err)
	lastAcceptedHeight := uint64(1)

	t.Run("adding first block", func(t *testing.T) {
		require := require.New(t)

		missingIDs := set.Of(blocks[6].ID())
		parentID, shouldFetchParentID, err := process(
			db,
			tree,
			missingIDs,
			lastAcceptedHeight,
			blocks[6],
			map[ids.ID]snowman.Block{
				blocks[2].ID(): blocks[2],
			},
		)
		require.NoError(err)
		require.True(shouldFetchParentID)
		require.Equal(blocks[5].ID(), parentID)
		require.Equal(uint64(1), tree.Len())
		require.Empty(missingIDs)
	})

	t.Run("adding multiple blocks", func(t *testing.T) {
		require := require.New(t)

		missingIDs := set.Of(blocks[5].ID())
		parentID, shouldFetchParentID, err := process(
			db,
			tree,
			missingIDs,
			lastAcceptedHeight,
			blocks[5],
			map[ids.ID]snowman.Block{
				blocks[4].ID(): blocks[4],
				blocks[3].ID(): blocks[3],
			},
		)
		require.NoError(err)
		require.True(shouldFetchParentID)
		require.Equal(blocks[2].ID(), parentID)
		require.Equal(uint64(4), tree.Len())
		require.Empty(missingIDs)
	})

	t.Run("do not request last accepted block", func(t *testing.T) {
		require := require.New(t)

		missingIDs := set.Of(blocks[2].ID())
		_, shouldFetchParentID, err := process(
			db,
			tree,
			missingIDs,
			lastAcceptedHeight,
			blocks[2],
			nil,
		)
		require.NoError(err)
		require.False(shouldFetchParentID)
		require.Equal(uint64(5), tree.Len())
		require.Empty(missingIDs)
	})
}

func TestExecute(t *testing.T) {
	require := require.New(t)

	const numBlocks = 2*max(batchWritePeriod, iteratorReleasePeriod) + 1
	blocks := generateBlockchain(numBlocks)
	parser := makeParser(blocks)

	db := memdb.New()
	tree, err := interval.NewTree(db)
	require.NoError(err)
	const lastAcceptedHeight = 1

	for i, block := range blocks[lastAcceptedHeight+1:] {
		newlyWantsParent, err := interval.Add(
			db,
			tree,
			lastAcceptedHeight,
			block.Height(),
			block.Bytes(),
		)
		require.NoError(err)
		require.False(newlyWantsParent)
		require.Equal(uint64(i+1), tree.Len())
	}

	require.NoError(execute(
		context.Background(),
		&common.Halter{},
		logging.NoLog{}.Info,
		db,
		parser,
		tree,
		lastAcceptedHeight,
	))

	for _, block := range blocks[lastAcceptedHeight+1:] {
		require.Equal(choices.Accepted, block.Status())
	}

	size, err := database.Count(db)
	require.NoError(err)
	require.Zero(size)
}

func TestExecuteExitsWhenHalted(t *testing.T) {
	require := require.New(t)

	blocks := generateBlockchain(7)
	parser := makeParser(blocks)

	db := memdb.New()
	tree, err := interval.NewTree(db)
	require.NoError(err)
	const lastAcceptedHeight = 1

	for i, block := range blocks[lastAcceptedHeight+1:] {
		newlyWantsParent, err := interval.Add(
			db,
			tree,
			lastAcceptedHeight,
			block.Height(),
			block.Bytes(),
		)
		require.NoError(err)
		require.False(newlyWantsParent)
		require.Equal(uint64(i+1), tree.Len())
	}

	startSize, err := database.Count(db)
	require.NoError(err)

	halter := common.Halter{}
	halter.Halt(context.Background())
	err = execute(
		context.Background(),
		&halter,
		logging.NoLog{}.Info,
		db,
		parser,
		tree,
		lastAcceptedHeight,
	)
	require.NoError(err)

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
	tree, err := interval.NewTree(db)
	require.NoError(err)
	const (
		lastAcceptedHeightWhenAdding    = 1
		lastAcceptedHeightWhenExecuting = 3
	)

	for i, block := range blocks[lastAcceptedHeightWhenAdding+1:] {
		newlyWantsParent, err := interval.Add(
			db,
			tree,
			lastAcceptedHeightWhenAdding,
			block.Height(),
			block.Bytes(),
		)
		require.NoError(err)
		require.False(newlyWantsParent)
		require.Equal(uint64(i+1), tree.Len())
	}

	require.NoError(execute(
		context.Background(),
		&common.Halter{},
		logging.NoLog{}.Info,
		db,
		parser,
		tree,
		lastAcceptedHeightWhenExecuting,
	))

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

type testParser func(context.Context, []byte) (snowman.Block, error)

func (f testParser) ParseBlock(ctx context.Context, bytes []byte) (snowman.Block, error) {
	return f(ctx, bytes)
}

func makeParser(blocks []snowman.Block) block.Parser {
	return testParser(func(_ context.Context, b []byte) (snowman.Block, error) {
		for _, block := range blocks {
			if bytes.Equal(b, block.Bytes()) {
				return block, nil
			}
		}
		return nil, database.ErrNotFound
	})
}
