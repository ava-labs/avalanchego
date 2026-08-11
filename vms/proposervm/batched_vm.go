// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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

	"github.com/ava-labs/avalanchego/vms/proposervm/state"
)

var _ block.BatchedChainVM = (*VM)(nil)

func (vm *VM) GetAncestors(
	ctx context.Context,
	blkID ids.ID,
	maxBlocksNum int,
	maxBlocksSize int,
	maxBlocksRetrievalTime time.Duration,
) ([][]byte, error) {
	// TODO(StephenButtolph): [VM.GetAncestors] is more performant when serving
	// post-fork blocks than repeated [VM.GetBlock] calls. Even if the inner vm
	// doesn't implement [block.BatchedChainVM] we should still use the
	// optimized path to serve post-fork blocks.
	if vm.batchedVM == nil {
		return nil, block.ErrRemoteVMNotImplemented
	}

	res := make([][]byte, 0, maxBlocksNum)
	currentByteLength := 0
	startTime := vm.Clock.Time()

	// Prefer the indexed walk, which resolves the whole height range up front
	// and reads the blocks concurrently. It only applies to accepted blocks;
	// anything else falls through to the serial walk below.
	if indexed, ok := vm.getAncestorsIndexed(ctx, blkID, maxBlocksNum, maxBlocksSize, startTime.Add(maxBlocksRetrievalTime)); ok {
		res = indexed
		for _, blkBytes := range res {
			currentByteLength += wrappers.IntLen + len(blkBytes)
		}
		if len(res) >= maxBlocksNum ||
			currentByteLength >= maxBlocksSize ||
			!vm.Clock.Time().Before(startTime.Add(maxBlocksRetrievalTime)) {
			return res, nil
		}
		// The indexed walk stops at the fork height, so continue from the
		// parent of the oldest block returned.
		parentID, err := statelessblock.ParentID(res[len(res)-1])
		if err != nil {
			return res, nil
		}
		blkID = parentID
	}

	// hereinafter loop over proposerVM cache and DB, possibly till snowman++
	// fork is hit
	for {
		// Only the bytes and the parent link are needed to serve the response,
		// so the block is deliberately not decoded here. See
		// [VM.getStatelessBlkBytes].
		blkBytes, parentID, err := vm.getStatelessBlkBytes(blkID)
		if err != nil {
			// maybe we have hit the proposerVM fork here?
			break
		}

		// Ensure response size isn't too large. Include wrappers.IntLen because
		// the size of the message is included with each container, and the size
		// is repr. by an int.
		currentByteLength += wrappers.IntLen + len(blkBytes)
		elapsedTime := vm.Clock.Time().Sub(startTime)
		if len(res) > 0 && (currentByteLength >= maxBlocksSize || maxBlocksRetrievalTime <= elapsedTime) {
			return res, nil // reached maximum size or ran out of time
		}

		res = append(res, blkBytes)
		blkID = parentID
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

// getAncestorsIndexed serves the post-fork portion of a GetAncestors response
// using the height index, which lets the block reads be issued concurrently
// instead of one per parent pointer.
//
// It reports false when the request cannot be served this way, in which case
// the caller must fall back to walking parent pointers. That happens when the
// requested block's height cannot be determined, or when the block is not the
// accepted block at that height - the height index describes only the accepted
// chain, so serving from it for any other block would return the wrong blocks.
func (vm *VM) getAncestorsIndexed(
	ctx context.Context,
	blkID ids.ID,
	maxBlocksNum int,
	maxBlocksSize int,
	deadline time.Time,
) ([][]byte, bool) {
	// One block is fetched the expensive way to learn its height; every other
	// block in the response is then read without decoding.
	blk, err := vm.getPostForkBlock(ctx, blkID)
	if err != nil {
		return nil, false
	}
	height := blk.Height()

	// Only the accepted chain is indexed by height.
	acceptedID, err := vm.State.GetBlockIDAtHeight(height)
	if err != nil || acceptedID != blkID {
		return nil, false
	}

	res, err := state.GetAncestorBytes(
		vm.State,
		vm.State,
		height,
		maxBlocksNum,
		maxBlocksSize,
		deadline,
		vm.Clock.Time,
		state.DefaultAncestorsConcurrency,
	)
	if err != nil || len(res) == 0 {
		return nil, false
	}
	return res, true
}

func (vm *VM) getStatelessBlk(blkID ids.ID) (statelessblock.Block, error) {
	if currentBlk, exists := vm.verifiedBlocks[blkID]; exists {
		return currentBlk.getStatelessBlk(), nil
	}
	return vm.State.GetBlock(blkID)
}

// getStatelessBlkBytes returns the serialized block along with the ID of its
// parent, which together are everything [VM.GetAncestors] needs to walk the
// chain and build its response.
//
// Blocks currently in consensus are already decoded, so they are served from
// memory as before. Blocks read from disk skip decoding entirely; see
// [state.BlockState.GetBlockBytesAndParent].
func (vm *VM) getStatelessBlkBytes(blkID ids.ID) ([]byte, ids.ID, error) {
	if currentBlk, exists := vm.verifiedBlocks[blkID]; exists {
		blk := currentBlk.getStatelessBlk()
		return blk.Bytes(), blk.ParentID(), nil
	}
	return vm.State.GetBlockBytesAndParent(blkID)
}
