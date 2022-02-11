// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	statelessblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

var _ block.BatchedChainVM = &VM{}

func (vm *VM) GetAncestors(
	blkID ids.ID,
	maxBlocksNum int,
	maxBlocksSize int,
	maxBlocksRetrivalTime time.Duration,
) ([][]byte, error) {
	rVM, ok := vm.ChainVM.(block.BatchedChainVM)
	if !ok {
		return nil, block.ErrRemoteVMNotImplemented
	}

	res := make([][]byte, 0, maxBlocksNum)
	currentByteLength := 0
	startTime := time.Now()

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
		if len(res) > 0 && (currentByteLength >= maxBlocksSize || maxBlocksRetrivalTime <= time.Since(startTime)) {
			return res, nil // reached maximum size or ran out of time
		}

		res = append(res, blkBytes)
		blkID = blk.ParentID()
		maxBlocksNum--

		if maxBlocksNum <= 0 {
			return res, nil
		}
	}

	// snowman++ fork may have been hit.
	preMaxBlocksNum := maxBlocksNum - len(res)
	preMaxBlocksSize := maxBlocksSize - currentByteLength
	preMaxBlocksRetrivalTime := maxBlocksRetrivalTime - time.Since(startTime)
	innerBytes, err := rVM.GetAncestors(blkID, preMaxBlocksNum, preMaxBlocksSize, preMaxBlocksRetrivalTime)
	if err != nil {
		if len(res) == 0 {
			return nil, err
		}
		return res, nil // return what we have
	}
	res = append(res, innerBytes...)
	return res, nil
}

func (vm *VM) BatchedParseBlock(blks [][]byte) ([]snowman.Block, error) {
	rVM, ok := vm.ChainVM.(block.BatchedChainVM)
	if !ok {
		return nil, block.ErrRemoteVMNotImplemented
	}

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
	for ; blocksIndex < len(blks); blocksIndex++ {
		blkBytes := blks[blocksIndex]
		statelessBlock, err := statelessblock.Parse(blkBytes)
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
	innerBlks, err := rVM.BatchedParseBlock(innerBlockBytes)
	if err != nil {
		return nil, err
	}
	for ; innerBlocksIndex < len(statelessBlockDescs); innerBlocksIndex++ {
		statelessBlockDesc := statelessBlockDescs[innerBlocksIndex]
		statelessBlk := statelessBlockDesc.block
		blkID := statelessBlk.ID()

		_, status, err := vm.State.GetBlock(blkID)
		if err == database.ErrNotFound {
			status = choices.Processing
		} else if err != nil {
			return nil, err
		}

		if statelessSignedBlock, ok := statelessBlk.(statelessblock.SignedBlock); ok {
			blocks[statelessBlockDesc.index] = &postForkBlock{
				SignedBlock: statelessSignedBlock,
				postForkCommonComponents: postForkCommonComponents{
					vm:       vm,
					innerBlk: innerBlks[innerBlocksIndex],
					status:   status,
				},
			}
		} else {
			blocks[statelessBlockDesc.index] = &postForkOption{
				Block: statelessBlk,
				postForkCommonComponents: postForkCommonComponents{
					vm:       vm,
					innerBlk: innerBlks[innerBlocksIndex],
					status:   status,
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
	statelessBlock, _, err := vm.State.GetBlock(blkID)
	return statelessBlock, err
}
