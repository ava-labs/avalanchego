// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// warptest exposes common functionality for testing the warp package.
package warptest

import (
	"context"
	"slices"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
)

// EmptyBlockStore returns an error if a block is requested
var EmptyBlockStore BlockStore = MakeBlockStore()

type BlockStore func(ctx context.Context, blockID ids.ID) (snowman.Block, error)

func (b BlockStore) GetBlock(ctx context.Context, blockID ids.ID) (snowman.Block, error) {
	return b(ctx, blockID)
}

// MakeBlockStore returns a new BlockStore that returns the provided blocks.
// If a block is requested that isn't part of the provided blocks, an error is
// returned.
func MakeBlockStore(blkIDs ...ids.ID) BlockStore {
	return func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		if !slices.Contains(blkIDs, blkID) {
			return nil, database.ErrNotFound
		}

		return &snowmantest.Block{
			Decidable: snowtest.Decidable{
				IDV:    blkID,
				Status: snowtest.Accepted,
			},
		}, nil
	}
}
