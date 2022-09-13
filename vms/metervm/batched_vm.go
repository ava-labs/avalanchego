// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

func (vm *blockVM) GetAncestors(
	blkID ids.ID,
	maxBlocksNum int,
	maxBlocksSize int,
	maxBlocksRetrivalTime time.Duration,
) ([][]byte, error) {
	if vm.bVM == nil {
		return nil, block.ErrRemoteVMNotImplemented
	}

	start := vm.clock.Time()
	ancestors, err := vm.bVM.GetAncestors(
		blkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
	end := vm.clock.Time()
	vm.blockMetrics.getAncestors.Observe(float64(end.Sub(start)))
	return ancestors, err
}

func (vm *blockVM) BatchedParseBlock(blks [][]byte) ([]snowman.Block, error) {
	if vm.bVM == nil {
		return nil, block.ErrRemoteVMNotImplemented
	}

	start := vm.clock.Time()
	blocks, err := vm.bVM.BatchedParseBlock(blks)
	end := vm.clock.Time()
	vm.blockMetrics.batchedParseBlock.Observe(float64(end.Sub(start)))

	wrappedBlocks := make([]snowman.Block, len(blocks))
	for i, block := range blocks {
		wrappedBlocks[i] = &meterBlock{
			Block: block,
			vm:    vm,
		}
	}
	return wrappedBlocks, err
}
