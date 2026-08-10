// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"golang.org/x/sync/errgroup"

	_ "github.com/ava-labs/avalanchego/snow/engine/snowman/block" // for comment resolution

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
)

// BatchedParseBlock parses the given blocks concurrently. It returns an error
// if any of the blocks fail to parse.
func (vm *VM) BatchedParseBlock(ctx context.Context, blks [][]byte) ([]*blocks.Block, error) {
	var (
		eg     errgroup.Group
		parsed = make([]*blocks.Block, len(blks))
	)
	eg.SetLimit(runtime.GOMAXPROCS(0))
	for i, buf := range blks {
		eg.Go(func() error {
			b, err := vm.ParseBlock(ctx, buf)
			parsed[i] = b
			return err
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	return parsed, nil
}

// GetAncestors returns the blocks starting with the block with the given ID and
// continuing with its ancestors, up to the given maximum number of blocks or
// maximum total size of blocks. The returned blocks are in order from the
// requested block to its ancestors. Only accepted blocks are served, any other
// block is treated as not found. For more details about guarantees, see
// [block.GetAncestors].
func (vm *VM) GetAncestors(
	ctx context.Context,
	blkID ids.ID,
	maxBlocksNum int,
	maxBlocksSize int,
	maxBlocksRetrievalTime time.Duration,
) ([][]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, maxBlocksRetrievalTime)
	defer cancel()

	hash := common.Hash(blkID)
	requestedNum := rawdb.ReadHeaderNumber(vm.db, hash)
	if requestedNum == nil {
		return nil, nil // hash is not accepted
	}
	num := *requestedNum
	if rawdb.ReadCanonicalHash(vm.db, *requestedNum) != hash {
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

		header := rawdb.ReadHeaderRLP(vm.db, hash, num)
		if header == nil {
			break
		}
		body := rawdb.ReadBodyRLP(vm.db, hash, num)
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
