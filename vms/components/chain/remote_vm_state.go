package chain

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// In this file methods specific of RemoteVM are implemented

func (s *State) GetAncestors(
	blkID ids.ID,
	maxBlocksNum int,
	maxBlocksSize int,
	maxBlocksRetrivalTime time.Duration,
) [][]byte {
	res := make([][]byte, 0, maxBlocksNum)
	currentByteLength := 0
	startTime := time.Now()
	var currentDuration time.Duration

	for cnt := 0; ; cnt++ {
		currentDuration = time.Since(startTime)
		if cnt >= maxBlocksNum || currentDuration >= maxBlocksRetrivalTime {
			return res // return what we have
		}

		blk, ok := s.getCachedBlock(blkID)
		if !ok {
			if _, ok := s.missingBlocks.Get(blkID); ok {
				// currently requested block is not cached nor available over wire.
				return res // return what we have
			}

			// currently requested block and its ancestors may be available over wire
			break // done with cache, move to wire
		}

		blkBytes := blk.Bytes()
		// Ensure response size isn't too large. Include wrappers.IntLen because the size of the message
		// is included with each container, and the size is repr. by an int.
		if newLen := currentByteLength + wrappers.IntLen + len(blkBytes); newLen < maxBlocksSize {
			res = append(res, blkBytes)
			currentByteLength = newLen
			blkID = blk.Parent()
			continue
		}

		// reached maximum response size, return what we have
		return res
	}

	wireBlkID := blkID
	wireMaxBlkNum := maxBlocksNum - len(res)
	wireMaxBlocksSize := maxBlocksSize - currentByteLength
	wireMaxBlocksRetrivalTime := time.Duration(maxBlocksRetrivalTime.Nanoseconds() - currentDuration.Nanoseconds())

	wireRes := s.getAncestors(wireBlkID, wireMaxBlkNum, wireMaxBlocksSize, wireMaxBlocksRetrivalTime)
	res = append(res, wireRes...)
	return res
}

func (s *State) BatchedParseBlock(blksBytes [][]byte) ([]snowman.Block, error) {
	res := make([]snowman.Block, len(blksBytes))
	remoteReqs := make([][]byte, 0, len(blksBytes)) // blks to be parsed by remoteVM
	missingIdxs := make([]int, 0, len(blksBytes))   // idxs of blks to be parsed by remoteVM
	for idx, blkBytes := range blksBytes {
		// See if we've cached this block's ID by its byte repr.
		blkIDIntf, blkIDCached := s.bytesToIDCache.Get(string(blkBytes))
		if blkIDCached {
			blkID := blkIDIntf.(ids.ID)
			// See if we have this block cached
			if cachedBlk, ok := s.getCachedBlock(blkID); ok {
				res[idx] = cachedBlk
				continue // move on to next blk
			}
		}

		// We don't have this block cached by its byte repr.
		// Add to those to ask to RemoteVM
		missingIdxs = append(missingIdxs, idx)
		remoteReqs = append(remoteReqs, blkBytes)
	}

	// We don't have this block cached by its byte repr.
	// Parse the block from bytes
	respBlks, err := s.batchedParseBlock(remoteReqs)
	if err != nil {
		return nil, err
	}
	if len(remoteReqs) != len(respBlks) {
		return nil, fmt.Errorf("BatchedParse block returned different number of blocks than expected")
	}

	for idx, respBlk := range respBlks {
		blkID := respBlk.ID()
		blkBytes := respBlk.Bytes()
		s.bytesToIDCache.Put(string(blkBytes), blkID)

		// Check for an existing block, so we can return a unique block
		// if processing or simply allow this block to be immediately
		// garbage collected if it is already cached.
		if cachedBlk, ok := s.getCachedBlock(blkID); ok {
			missingIdx := missingIdxs[idx]
			res[missingIdx] = cachedBlk
			continue
		}

		s.missingBlocks.Evict(blkID)
		res[idx], err = s.addBlockOutsideConsensus(respBlk)
		if err != nil {
			return nil, err
		}
	}

	return res, nil
}
