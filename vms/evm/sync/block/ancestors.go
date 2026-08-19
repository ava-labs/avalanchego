// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// GetAncestors returns the blocks starting with the block with the given ID and
// continuing with its ancestors, up to the given maximum number of blocks or
// maximum total size of blocks. The returned blocks are in order from the
// requested block to its ancestors. Only accepted blocks are served, any other
// block is treated as not found. For more details about guarantees, see
// [github.com/ava-labs/avalanchego/snow/engine/snowman/block.GetAncestors].
//
// TODO(StephenButtolph): Expose this on the VM to back
// [github.com/ava-labs/avalanchego/snow/engine/snowman/block.BatchedChainVM], so
// the consensus engine and the sync handler share one walk.
func GetAncestors(
	ctx context.Context,
	db ethdb.Reader,
	blkID ids.ID,
	maxBlocksNum int,
	maxBlocksSize int,
	maxBlocksRetrievalTime time.Duration,
) ([][]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, maxBlocksRetrievalTime)
	defer cancel()

	hash := common.Hash(blkID)
	requestedNum := rawdb.ReadHeaderNumber(db, hash)
	if requestedNum == nil {
		return nil, nil // hash is not accepted
	}
	num := *requestedNum
	if rawdb.ReadCanonicalHash(db, *requestedNum) != hash {
		return nil, nil // requested block is not canonical
	}

	// TODO(StephenButtolph): Measure the performance impact of iterative
	// fetching rather than using DB iterators on real databases.
	var (
		numBlocks = min(
			uint64(max(maxBlocksNum, 1)), //#nosec G115 -- non-negative by max()
			num+1,
		)
		blocks = make([][]byte, 0, numBlocks)
		size   int
	)
	for range numBlocks {
		// Returning no blocks reports to the caller that we don't have the
		// requested block. Even if we have exceeded the time limit, we should
		// still attempt to return at least the requested block if it exists.
		if len(blocks) > 0 && ctx.Err() != nil {
			break
		}

		header := rawdb.ReadHeaderRLP(db, hash, num)
		if header == nil {
			break
		}
		body := rawdb.ReadBodyRLP(db, hash, num)
		if body == nil {
			break
		}
		block, err := types.BlockBytes(header, body)
		if err != nil {
			return nil, fmt.Errorf("splicing stored block %d: %v", num, err)
		}

		// Even if the first block exceeds maxBlocksSize, we still return it to
		// support very large blocks.
		size += len(block) + wrappers.IntLen
		if len(blocks) > 0 && size > maxBlocksSize {
			break
		}
		blocks = append(blocks, block)

		// It is possible for the last iteration to underflow num, but the loop
		// will exit before reading num again.
		num--
		hash = types.HeaderParentHashFromRLP(header)
	}
	return blocks, nil
}
