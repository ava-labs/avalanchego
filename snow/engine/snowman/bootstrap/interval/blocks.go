// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interval

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

// Add the block to the tree and return if the parent block should be fetched,
// but wasn't desired before.
func Add(
	db database.KeyValueWriterDeleter,
	tree *Tree,
	lastAcceptedHeight uint64,
	blk snowman.Block,
) (bool, error) {
	var (
		height            = blk.Height()
		lastHeightToFetch = lastAcceptedHeight + 1
	)
	if height < lastHeightToFetch || tree.Contains(height) {
		return false, nil
	}

	blkBytes := blk.Bytes()
	if err := PutBlock(db, height, blkBytes); err != nil {
		return false, err
	}

	if err := tree.Add(db, height); err != nil {
		return false, err
	}

	return height != lastHeightToFetch && !tree.Contains(height-1), nil
}

// Remove the block from the tree.
func Remove(
	db database.KeyValueWriterDeleter,
	tree *Tree,
	height uint64,
) error {
	if err := DeleteBlock(db, height); err != nil {
		return err
	}
	return tree.Remove(db, height)
}
