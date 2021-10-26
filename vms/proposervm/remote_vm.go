package proposervm

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	statelessblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

func (vm *VM) BatchedParseBlock(blks [][]byte) ([]snowman.Block, error) {
	rVM, ok := vm.ChainVM.(block.RemoteVM)
	if !ok {
		return nil, block.ErrRemoteVMNotImplemented
	}
	innerBlks := make([][]byte, 0, len(blks))
	for _, outerBlkBytes := range blks {
		var innerBlkBytes []byte
		if statelessBlock, err := statelessblock.Parse(outerBlkBytes); err == nil {
			innerBlkBytes = statelessBlock.Block() // Retrieve core bytes
		} else {
			// assume it is a preForkBlock and defer parsing to VM
			innerBlkBytes = outerBlkBytes
		}
		innerBlks = append(innerBlks, innerBlkBytes)
	}
	return rVM.BatchedParseBlock(innerBlks)
}

func (vm *VM) GetAncestors(
	blkID ids.ID,
	maxBlocksNum int,
	maxBlocksSize int,
	maxBlocksRetrivalTime time.Duration,
) ([][]byte, error) {
	rVM, ok := vm.ChainVM.(block.RemoteVM)
	if !ok {
		return nil, block.ErrRemoteVMNotImplemented
	}

	_, errBlk := vm.getPostForkBlock(blkID)
	if errBlk != nil {
		// assume it is a preForkBlock
		return rVM.GetAncestors(blkID, maxBlocksNum, maxBlocksSize, maxBlocksRetrivalTime)
	}

	// hereinafter loop over proposerVM cache and DB, possibly till snowman++ fork is hit
	res := make([][]byte, 0, maxBlocksNum)
	currentByteLength := 0
	var currentDuration time.Duration
	startTime := time.Now()

	for cnt := 0; ; cnt++ {
		currentDuration = time.Since(startTime)
		if cnt >= maxBlocksNum || currentDuration >= maxBlocksRetrivalTime {
			return res, nil // return what we have
		}

		var blkBytes []byte
		if currentBlk, exists := vm.verifiedBlocks[blkID]; !exists {
			statelessBlock, _, err := vm.State.GetBlock(blkID)
			if err != nil {
				// maybe we have hit the proposerVM fork here?
				break
			}
			blkBytes = statelessBlock.Bytes()
			blkID = statelessBlock.ParentID() // set next blkID to look for
		} else {
			blkBytes = currentBlk.Bytes()
			blkID = currentBlk.Parent() // set next blkID to look for
		}

		// Ensure response size isn't too large. Include wrappers.IntLen because the size of the message
		// is included with each container, and the size is repr. by an int.
		if newLen := currentByteLength + wrappers.IntLen + len(blkBytes); newLen < maxBlocksSize {
			res = append(res, blkBytes)
			currentByteLength = newLen
			continue
		}

		return res, nil // reached maximum size
	}

	if _, err := vm.getPreForkBlock(blkID); err != nil {
		return res, nil // return what we have
	}

	// snowman++ fork hit.
	preMaxBlocksNum := maxBlocksNum - len(res)
	preMaxBlocksSize := maxBlocksSize - currentByteLength
	preMaxBlocksRetrivalTime := maxBlocksRetrivalTime - currentDuration
	innerBytes, err := rVM.GetAncestors(blkID, preMaxBlocksNum, preMaxBlocksSize, preMaxBlocksRetrivalTime)
	if err != nil {
		return res, nil // return what we have
	}
	res = append(res, innerBytes...)
	return res, nil
}
