// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interval

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/set"
)

type Parser interface {
	ParseBlock(context.Context, []byte) (snowman.Block, error)
}

func GetMissingBlockIDs(
	ctx context.Context,
	parser Parser,
	tree *Tree,
	lastAcceptedHeight uint64,
) (set.Set[ids.ID], error) {
	var (
		missingBlocks     set.Set[ids.ID]
		intervals         = tree.Flatten()
		lastHeightToFetch = lastAcceptedHeight + 1
	)
	for _, i := range intervals {
		if i.LowerBound <= lastHeightToFetch {
			continue
		}

		blkBytes, err := GetBlock(tree.db, i.LowerBound)
		if err != nil {
			return nil, err
		}

		blk, err := parser.ParseBlock(ctx, blkBytes)
		if err != nil {
			return nil, err
		}

		parentID := blk.Parent()
		missingBlocks.Add(parentID)
	}
	return missingBlocks, nil
}

// Add the block to the tree and return if the parent block should be fetched.
func Add(tree *Tree, lastAcceptedHeight uint64, blk snowman.Block) (bool, error) {
	var (
		height            = blk.Height()
		lastHeightToFetch = lastAcceptedHeight + 1
	)
	if height < lastHeightToFetch || tree.Contains(height) {
		return false, nil
	}

	blkBytes := blk.Bytes()
	if err := PutBlock(tree.db, height, blkBytes); err != nil {
		return false, err
	}

	if err := tree.Add(height); err != nil {
		return false, err
	}

	return height != lastHeightToFetch && !tree.Contains(height-1), nil
}

func Execute(
	ctx context.Context,
	parser Parser,
	tree *Tree,
	lastAcceptedHeight uint64,
) error {
	it := tree.db.NewIteratorWithPrefix(blockPrefix)
	defer func() {
		it.Release()
	}()

	// TODO: Periodically release the iterator here
	// TODO: Periodically log progress
	// TODO: Add metrics
	for it.Next() {
		blkBytes := it.Value()
		blk, err := parser.ParseBlock(ctx, blkBytes)
		if err != nil {
			return err
		}

		height := blk.Height()
		if err := DeleteBlock(tree.db, height); err != nil {
			return err
		}

		if err := tree.Remove(height); err != nil {
			return err
		}

		if height <= lastAcceptedHeight {
			continue
		}

		if err := blk.Verify(ctx); err != nil {
			return err
		}
		if err := blk.Accept(ctx); err != nil {
			return err
		}
	}
	return it.Error()
}
