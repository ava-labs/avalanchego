// (c) 2021, Ava Labs, Inc. All rights reserved.
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

	res := make([]snowman.Block, len(blks))
	type partialData struct {
		statelessblock.Block
		innerBytes []byte
	}
	blksToBeCompleted := make(map[int]partialData)
	for idx, blkBytes := range blks {
		if statelessBlock, err := statelessblock.Parse(blkBytes); err == nil {
			blkID := statelessBlock.ID()
			if blk, err := vm.getPostForkBlock(blkID); err == nil {
				// blk already known, move on.
				res[idx] = blk
				continue
			}
			if err != database.ErrNotFound {
				// blk not known. Batch-parse innerBytes and then batch-build
				blksToBeCompleted[idx] = partialData{
					Block:      statelessBlock,
					innerBytes: statelessBlock.Block(),
				}
				continue
			}
			return nil, err
		}

		// assume it is a preForkBlock and defer parsing to VM
		blksToBeCompleted[idx] = partialData{
			innerBytes: blkBytes,
		}
	}

	missingIdxs := make([]int, 0, len(blksToBeCompleted))
	innerBlksBytes := make([][]byte, 0, len(blksToBeCompleted))

	for idx, data := range blksToBeCompleted {
		missingIdxs = append(missingIdxs, idx)
		innerBlksBytes = append(innerBlksBytes, data.innerBytes)
	}

	// parse all inner blocks at once
	innerBlks, err := rVM.BatchedParseBlock(innerBlksBytes)
	if err != nil {
		return res, err
	}

	// duly rebuild ProposerVM blocks given all the innerBlks
	for idx, innerBlk := range innerBlks {
		var blk snowman.Block
		missingIdx := missingIdxs[idx]
		if statelessBlock := blksToBeCompleted[idx].Block; statelessBlock != nil {
			// build postForkBlock given statelessBlk and innerBlk
			if statelessSignedBlock, ok := statelessBlock.(statelessblock.SignedBlock); ok {
				blk = &postForkBlock{
					SignedBlock: statelessSignedBlock,
					postForkCommonComponents: postForkCommonComponents{
						vm:       vm,
						innerBlk: innerBlk,
						status:   choices.Processing,
					},
				}
			} else {
				blk = &postForkOption{
					Block: statelessBlock,
					postForkCommonComponents: postForkCommonComponents{
						vm:       vm,
						innerBlk: innerBlk,
						status:   choices.Processing,
					},
				}
			}
		} else {
			blk = &preForkBlock{
				Block: innerBlk,
				vm:    vm,
			}
		}
		res[missingIdx] = blk
	}

	return res, nil
}

func (vm *VM) getStatelessBlk(blkID ids.ID) (statelessblock.Block, error) {
	if currentBlk, exists := vm.verifiedBlocks[blkID]; exists {
		return currentBlk.getStatelessBlk(), nil
	}
	statelessBlock, _, err := vm.State.GetBlock(blkID)
	return statelessBlock, err
}
