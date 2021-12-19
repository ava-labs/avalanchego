// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var _ block.BatchedChainVM = &blockVM{}

func (vm *blockVM) GetAncestors(
	blkID ids.ID,
	maxBlocksNum int,
	maxBlocksSize int,
	maxBlocksRetrivalTime time.Duration,
) ([][]byte, error) {
	rVM, ok := vm.ChainVM.(block.BatchedChainVM)
	if !ok {
		return nil, block.ErrRemoteVMNotImplemented
	}

	start := vm.clock.Time()
	ancestors, err := rVM.GetAncestors(
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
	rVM, ok := vm.ChainVM.(block.BatchedChainVM)
	if !ok {
		return nil, block.ErrRemoteVMNotImplemented
	}

	start := vm.clock.Time()
	blocks, err := rVM.BatchedParseBlock(blks)
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
