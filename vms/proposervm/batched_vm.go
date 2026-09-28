// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	statelessblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

const (
	// initialAncestorsReadBatchSize is the number of accepted blocks read
	// concurrently before the average block size of the response is known.
	initialAncestorsReadBatchSize = 16
	// maxAncestorsReadBatchSize bounds the number of accepted blocks read
	// concurrently. The size limit is only checked between batches, so it
	// also bounds the number of reads that are wasted once that limit is
	// reached.
	maxAncestorsReadBatchSize = 256
)

var (
	_ block.BatchedChainVM = (*VM)(nil)

	errUnexpectedBlockAtHeight = errors.New("unexpected block at height")
)

// GetAncestors returns the block with the given ID followed by its ancestors,
// bounded by maxBlocksNum, maxBlocksSize, and maxBlocksRetrievalTime.
//
// Post-fork blocks are served from this VM's state regardless of whether the
// inner VM implements [block.BatchedChainVM]. Blocks that are verified but not
// yet accepted are walked by parent ID in memory. Accepted blocks are resolved
// through the height index and read as raw bytes concurrently, which avoids
// parsing every block and issuing sequential hash-keyed database reads.
//
// Pre-fork blocks are delegated to the inner VM if it implements
// [block.BatchedChainVM]. Otherwise, the post-fork blocks that were served are
// returned, or [block.ErrRemoteVMNotImplemented] if there were none so that
// the caller falls back to [block.ChainVM.GetBlock].
func (vm *VM) GetAncestors(
	ctx context.Context,
	blkID ids.ID,
	maxBlocksNum int,
	maxBlocksSize int,
	maxBlocksRetrievalTime time.Duration,
) ([][]byte, error) {
	maxBlocksNum = max(maxBlocksNum, 1)
	res := &ancestorsResponse{
		blocks:   make([][]byte, 0, min(maxBlocksNum, maxAncestorsReadBatchSize)),
		maxNum:   maxBlocksNum,
		maxSize:  maxBlocksSize,
		deadline: vm.Clock.Time().Add(maxBlocksRetrievalTime),
		clock:    &vm.Clock,
	}

	// Blocks that are verified but not yet accepted are only held in memory.
	for {
		blk, ok := vm.verifiedBlocks[blkID]
		if !ok {
			break
		}
		if !res.add(blk.Bytes()) {
			return res.blocks, nil
		}
		blkID = blk.Parent()
	}

	nextID, hasRoom, err := vm.appendAcceptedAncestors(ctx, res, blkID)
	if err != nil {
		return nil, err
	}
	if !hasRoom {
		return res.blocks, nil
	}

	// The next block is either pre-fork or unknown to the proposervm, so it
	// can only be served by the inner VM.
	if vm.batchedVM == nil {
		if len(res.blocks) == 0 {
			return nil, block.ErrRemoteVMNotImplemented
		}
		return res.blocks, nil
	}
	innerBytes, err := vm.batchedVM.GetAncestors(
		ctx,
		nextID,
		maxBlocksNum-len(res.blocks),
		maxBlocksSize-res.size,
		res.deadline.Sub(vm.Clock.Time()),
	)
	if err != nil {
		if len(res.blocks) == 0 {
			return nil, err
		}
		return res.blocks, nil // return what we have
	}
	return append(res.blocks, innerBytes...), nil
}

// appendAcceptedAncestors appends the accepted post-fork block with the given
// ID and its accepted ancestors to res.
//
// It returns the ID of the first block that wasn't served, which is blkID
// itself if it isn't an accepted post-fork block stored by this VM or the
// parent of the last served block otherwise, and whether res can accept more
// blocks.
func (vm *VM) appendAcceptedAncestors(
	ctx context.Context,
	res *ancestorsResponse,
	blkID ids.ID,
) (ids.ID, bool, error) {
	if !res.hasRoom() {
		return blkID, false, nil
	}

	// Only accepted blocks are stored, so fetching the block both confirms
	// that it is accepted and provides its height, which is needed to look up
	// its ancestors in the height index.
	blk, err := vm.getPostForkBlock(ctx, blkID)
	if errors.Is(err, database.ErrNotFound) {
		return blkID, true, nil
	}
	if err != nil {
		return ids.Empty, false, err
	}

	forkHeight, err := vm.State.GetForkHeight()
	if err != nil {
		return ids.Empty, false, fmt.Errorf("failed to get fork height: %w", err)
	}

	// Resolve the IDs of every block that may fit in the response with a
	// single scan of the height index. Blocks below the fork height are not
	// stored by the proposervm.
	var (
		maxHeight = blk.Height()
		// hasRoom guarantees that at least one more block fits.
		remaining = uint64(res.maxNum - len(res.blocks)) //#nosec G115 -- positive by hasRoom()
		minHeight = min(
			maxHeight,
			max(forkHeight, maxHeight-min(remaining-1, maxHeight)),
		)
	)
	blkIDs, err := vm.State.GetBlockIDsAtHeights(minHeight, maxHeight)
	if err != nil {
		return ids.Empty, false, err
	}
	slices.Reverse(blkIDs) // order from the requested block to its ancestors

	if blkIDs[0] != blkID {
		return ids.Empty, false, fmt.Errorf("%w %d: expected %s but found %s",
			errUnexpectedBlockAtHeight,
			maxHeight,
			blkID,
			blkIDs[0],
		)
	}

	// Only a contiguous run of indexed heights can be served. The index may
	// have gaps below the state sync target or the oldest retained block.
	if i := slices.Index(blkIDs, ids.Empty); i != -1 {
		blkIDs = blkIDs[:i]
	}

	var (
		numServed  int
		lastServed []byte
		batchSize  = initialAncestorsReadBatchSize
	)
	for numServed < len(blkIDs) {
		// The requested block is always served, even after the time limit,
		// so the reads only stop early once the response isn't empty.
		stopWhenTimedOut := len(res.blocks) > 0
		timedOut := func() bool {
			return stopWhenTimedOut && res.timedOut()
		}

		batchIDs := blkIDs[numServed:min(numServed+batchSize, len(blkIDs))]
		batchBytes, err := vm.getBlocksBytes(batchIDs, timedOut)
		if err != nil {
			return ids.Empty, false, err
		}

		for _, blkBytes := range batchBytes {
			if !res.add(blkBytes) {
				return ids.Empty, false, nil
			}
			numServed++
			lastServed = blkBytes
		}

		// A block that was indexed but not stored ends the run of blocks
		// that can be served.
		if len(batchBytes) < len(batchIDs) {
			break
		}

		// Size the next batch by the number of blocks that are expected to
		// fit in the remaining size limit, so that reaching the limit wastes
		// few reads.
		avgBlockSize := res.size / len(res.blocks)
		batchSize = min(
			maxAncestorsReadBatchSize,
			max(1, (res.maxSize-res.size)/avgBlockSize+1),
		)
	}
	if numServed == 0 {
		return blkID, true, nil
	}

	// The parent of the last served block is either pre-fork or not stored by
	// this VM. Parsing a single block to find its parent ID is cheap.
	lastServedBlk, err := statelessblock.ParseWithoutVerification(lastServed)
	if err != nil {
		return ids.Empty, false, err
	}
	return lastServedBlk.ParentID(), true, nil
}

// getBlocksBytes reads the stored bytes of the given accepted blocks
// concurrently. Reads stop once timedOut reports true, so that the time limit
// of a response is overshot by at most one read per worker. The result is
// ordered like blkIDs and truncated at the first block that isn't stored or
// wasn't read.
func (vm *VM) getBlocksBytes(blkIDs []ids.ID, timedOut func() bool) ([][]byte, error) {
	var (
		blocks     = make([][]byte, len(blkIDs))
		errs       = make([]error, len(blkIDs))
		read       = make([]bool, len(blkIDs))
		numWorkers = min(runtime.GOMAXPROCS(0), len(blkIDs))
		wg         sync.WaitGroup
	)
	// A single read is too cheap to justify a goroutine per block, so each
	// worker reads every numWorkers-th block instead.
	for worker := range numWorkers {
		wg.Go(func() {
			for i := worker; i < len(blkIDs); i += numWorkers {
				if timedOut() {
					return
				}
				blocks[i], errs[i] = vm.State.GetBlockBytes(blkIDs[i])
				read[i] = true
			}
		})
	}
	wg.Wait()

	for i, err := range errs {
		if !read[i] || err == database.ErrNotFound {
			return blocks[:i], nil
		}
		if err != nil {
			return nil, err
		}
	}
	return blocks, nil
}

// ancestorsResponse accumulates the blocks of a GetAncestors response while
// enforcing its limits.
type ancestorsResponse struct {
	blocks   [][]byte
	size     int
	maxNum   int
	maxSize  int
	deadline time.Time
	clock    *mockable.Clock
}

// add appends blkBytes to the response unless doing so would exceed the size
// limit or the retrieval time has elapsed. The first block is always added so
// that a single oversized block can still be served. It reports whether the
// response can accept more blocks.
func (r *ancestorsResponse) add(blkBytes []byte) bool {
	// Include wrappers.IntLen because the size of the message is included with
	// each container, and the size is repr. by an int.
	newSize := r.size + wrappers.IntLen + len(blkBytes)
	if len(r.blocks) > 0 && (newSize >= r.maxSize || r.timedOut()) {
		return false
	}
	r.blocks = append(r.blocks, blkBytes)
	r.size = newSize
	return r.hasRoom()
}

// hasRoom reports whether the response can accept more blocks. An empty
// response can always accept a block so that the requested block is served
// even if the retrieval time has already elapsed.
func (r *ancestorsResponse) hasRoom() bool {
	if len(r.blocks) == 0 {
		return true
	}
	return len(r.blocks) < r.maxNum && !r.timedOut()
}

func (r *ancestorsResponse) timedOut() bool {
	return !r.clock.Time().Before(r.deadline)
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
