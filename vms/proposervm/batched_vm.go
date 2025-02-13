// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	statelessblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

var _ block.BatchedChainVM = (*VM)(nil)

func (vm *VM) GetAncestors(
	ctx context.Context,
	blkID ids.ID,
	maxBlocksNum int,
	maxBlocksSize int,
	maxBlocksRetrievalTime time.Duration,
) ([][]byte, error) {
	if vm.batchedVM == nil {
		return nil, block.ErrRemoteVMNotImplemented
	}

	res := make([][]byte, 0, maxBlocksNum)
	currentByteLength := 0
	startTime := vm.Clock.Time()

	// hereinafter loop over proposerVM cache and DB, possibly till snowman++
	// fork is hit
	for {
		blk, err := vm.getStatelessBlk(blkID)
		if err != nil {
			// maybe we have hit the proposerVM fork here?
			break
		}

		blkBytes := blk.Bytes()

		// Ensure response size isn't too large. Include wrappers.IntLen because
		// the size of the message is included with each container, and the size
		// is repr. by an int.
		currentByteLength += wrappers.IntLen + len(blkBytes)
		elapsedTime := vm.Clock.Time().Sub(startTime)
		if len(res) > 0 && (currentByteLength >= maxBlocksSize || maxBlocksRetrievalTime <= elapsedTime) {
			return res, nil // reached maximum size or ran out of time
		}

		res = append(res, blkBytes)
		blkID = blk.ParentID()
		if len(res) >= maxBlocksNum {
			return res, nil
		}
	}

	// snowman++ fork may have been hit.
	preMaxBlocksNum := maxBlocksNum - len(res)
	preMaxBlocksSize := maxBlocksSize - currentByteLength
	preMaxBlocksRetrivalTime := maxBlocksRetrievalTime - time.Since(startTime)
	innerBytes, err := vm.batchedVM.GetAncestors(
		ctx,
		blkID,
		preMaxBlocksNum,
		preMaxBlocksSize,
		preMaxBlocksRetrivalTime,
	)
	if err != nil {
		if len(res) == 0 {
			return nil, err
		}
		return res, nil // return what we have
	}
	res = append(res, innerBytes...)
	return res, nil
}

func (vm *VM) BatchedParseBlock(ctx context.Context, blks [][]byte) ([]snowman.Block, error) {
	type partialData struct {
		index int
		block statelessblock.Block
	}
	var (
		blocksIndex int
		blocks      = make([]snowman.Block, len(blks))

		innerBlocksIndex    int
		statelessBlockDescs = make([]partialData, 0, len(blks))
		innerBlockBytes     = make([][]byte, 0, len(blks))
	)

	parsingResults := statelessblock.ParseBlocks(blks, vm.ctx.ChainID)

	for ; blocksIndex < len(blks); blocksIndex++ {
		statelessBlock, err := parsingResults[blocksIndex].Block, parsingResults[blocksIndex].Err
		if err != nil {
			break
		}

		blkID := statelessBlock.ID()
		block, exists := vm.verifiedBlocks[blkID]
		if exists {
			blocks[blocksIndex] = block
			continue
		}

		statelessBlockDescs = append(statelessBlockDescs, partialData{
			index: blocksIndex,
			block: statelessBlock,
		})
		innerBlockBytes = append(innerBlockBytes, statelessBlock.Block())
	}
	innerBlockBytes = append(innerBlockBytes, blks[blocksIndex:]...)

	// parse all inner blocks at once
	innerBlks, err := block.BatchedParseBlock(ctx, vm.ChainVM, innerBlockBytes)
	if err != nil {
		return nil, err
	}
	for ; innerBlocksIndex < len(statelessBlockDescs); innerBlocksIndex++ {
		statelessBlockDesc := statelessBlockDescs[innerBlocksIndex]
		statelessBlk := statelessBlockDesc.block

		if statelessSignedBlock, ok := statelessBlk.(statelessblock.SignedBlock); ok {
			blocks[statelessBlockDesc.index] = &postForkBlock{
				SignedBlock: statelessSignedBlock,
				postForkCommonComponents: postForkCommonComponents{
					vm:       vm,
					innerBlk: innerBlks[innerBlocksIndex],
				},
			}
		} else {
			blocks[statelessBlockDesc.index] = &postForkOption{
				Block: statelessBlk,
				postForkCommonComponents: postForkCommonComponents{
					vm:       vm,
					innerBlk: innerBlks[innerBlocksIndex],
				},
			}
		}
	}
	for ; blocksIndex < len(blocks); blocksIndex, innerBlocksIndex = blocksIndex+1, innerBlocksIndex+1 {
		blocks[blocksIndex] = &preForkBlock{
			Block: innerBlks[innerBlocksIndex],
			vm:    vm,
		}
	}
	return blocks, nil
}

func (vm *VM) getStatelessBlk(blkID ids.ID) (statelessblock.Block, error) {
	if currentBlk, exists := vm.verifiedBlocks[blkID]; exists {
		return currentBlk.getStatelessBlk(), nil
	}
	return vm.State.GetBlock(blkID)
}
