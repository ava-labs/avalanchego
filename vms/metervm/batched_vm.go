// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

func (vm *blockVM) GetAncestors(
	ctx context.Context,
	blkID ids.ID,
	maxBlocksNum int,
	maxBlocksSize int,
	maxBlocksRetrivalTime time.Duration,
) ([][]byte, error) {
	if vm.batchedVM == nil {
		return nil, block.ErrRemoteVMNotImplemented
	}

	start := time.Now()
	ancestors, err := vm.batchedVM.GetAncestors(
		ctx,
		blkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
	vm.blockMetrics.getAncestors.Observe(float64(time.Since(start)))
	return ancestors, err
}

func (vm *blockVM) BatchedParseBlock(ctx context.Context, blks [][]byte) ([]snowman.Block, error) {
	if vm.batchedVM == nil {
		return nil, block.ErrRemoteVMNotImplemented
	}

	start := time.Now()
	blocks, err := vm.batchedVM.BatchedParseBlock(ctx, blks)
	vm.blockMetrics.batchedParseBlock.Observe(float64(time.Since(start)))

	wrappedBlocks := make([]snowman.Block, len(blocks))
	for i, block := range blocks {
		wrappedBlocks[i] = &meterBlock{
			Block: block,
			vm:    vm,
		}
	}
	return wrappedBlocks, err
}
