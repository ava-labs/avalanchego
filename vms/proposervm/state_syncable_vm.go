// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"fmt"
	"sort"

	"go.uber.org/zap"
	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/summary"
)

func (vm *VM) StateSyncEnabled(ctx context.Context) (bool, error) {
	if vm.ssVM == nil {
		return false, nil
	}

	// if vm implements Snowman++, a block height index must be available
	// to support state sync
	if vm.VerifyHeightIndex(ctx) != nil {
		return false, nil
	}

	return vm.ssVM.StateSyncEnabled(ctx)
}

func (vm *VM) GetOngoingSyncStateSummary(ctx context.Context) (block.StateSummary, error) {
	if vm.ssVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

	innerSummary, err := vm.ssVM.GetOngoingSyncStateSummary(ctx)
	if err != nil {
		return nil, err // includes database.ErrNotFound case
	}

	return vm.buildStateSummary(ctx, innerSummary)
}

func (vm *VM) GetLastStateSummary(ctx context.Context) (block.StateSummary, error) {
	if vm.ssVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

	// Extract inner vm's last state summary
	innerSummary, err := vm.ssVM.GetLastStateSummary(ctx)
	if err != nil {
		return nil, err // including database.ErrNotFound case
	}

	return vm.buildStateSummary(ctx, innerSummary)
}

// Note: it's important that ParseStateSummary do not use any index or state
// to allow summaries being parsed also by freshly started node with no previous state.
func (vm *VM) ParseStateSummary(ctx context.Context, summaryBytes []byte) (block.StateSummary, error) {
	if vm.ssVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

	statelessSummary, err := summary.Parse(summaryBytes)
	if err != nil {
		// it may be a preFork summary
		return vm.ssVM.ParseStateSummary(ctx, summaryBytes)
	}

	innerSummary, err := vm.ssVM.ParseStateSummary(ctx, statelessSummary.InnerSummaryBytes())
	if err != nil {
		return nil, fmt.Errorf("could not parse inner summary due to: %w", err)
	}
	block, err := vm.parsePostForkBlock(ctx, statelessSummary.BlockBytes())
	if err != nil {
		return nil, fmt.Errorf("could not parse proposervm block bytes from summary due to: %w", err)
	}

	return &stateSummary{
		StateSummary: statelessSummary,
		innerSummary: innerSummary,
		block:        block,
		vm:           vm,
	}, nil
}

func (vm *VM) GetStateSummary(ctx context.Context, height uint64) (block.StateSummary, error) {
	if vm.ssVM == nil {
		return nil, block.ErrStateSyncableVMNotImplemented
	}

	innerSummary, err := vm.ssVM.GetStateSummary(ctx, height)
	if err != nil {
		return nil, err // including database.ErrNotFound case
	}

	return vm.buildStateSummary(ctx, innerSummary)
}

// Note: building state summary requires a well formed height index.
func (vm *VM) buildStateSummary(ctx context.Context, innerSummary block.StateSummary) (block.StateSummary, error) {
	// if vm implements Snowman++, a block height index must be available
	// to support state sync
	if err := vm.VerifyHeightIndex(ctx); err != nil {
		return nil, fmt.Errorf("could not build state summary: %w", err)
	}

	forkHeight, err := vm.GetForkHeight()
	switch err {
	case nil:
		if innerSummary.Height() < forkHeight {
			return innerSummary, nil
		}
	case database.ErrNotFound:
		// fork has not been reached since there is not fork height
		// just return innerSummary
		vm.ctx.Log.Debug("built pre-fork summary",
			zap.Stringer("summaryID", innerSummary.ID()),
			zap.Uint64("summaryHeight", innerSummary.Height()),
		)
		return innerSummary, nil
	default:
		return nil, err
	}

	height := innerSummary.Height()
	blkID, err := vm.GetBlockIDAtHeight(ctx, height)
	if err != nil {
		vm.ctx.Log.Debug("failed to fetch proposervm block ID",
			zap.Uint64("height", height),
			zap.Error(err),
		)
		return nil, err
	}
	block, err := vm.getPostForkBlock(ctx, blkID)
	if err != nil {
		vm.ctx.Log.Warn("failed to fetch proposervm block",
			zap.Stringer("blkID", blkID),
			zap.Uint64("height", height),
			zap.Error(err),
		)
		return nil, err
	}

	statelessSummary, err := summary.Build(forkHeight, block.Bytes(), innerSummary.Bytes())
	if err != nil {
		return nil, err
	}

	vm.ctx.Log.Debug("built post-fork summary",
		zap.Stringer("summaryID", statelessSummary.ID()),
		zap.Uint64("summaryHeight", forkHeight),
	)
	return &stateSummary{
		StateSummary: statelessSummary,
		innerSummary: innerSummary,
		block:        block,
		vm:           vm,
	}, nil
}

func (vm *VM) BackfillBlocksEnabled(ctx context.Context) (ids.ID, uint64, error) {
	if vm.ssVM == nil {
		return ids.Empty, 0, block.ErrBlockBackfillingNotEnabled
	}

	_, innerBlkHeight, err := vm.ssVM.BackfillBlocksEnabled(ctx)
	if err != nil {
		return ids.Empty, 0, fmt.Errorf("failed checking that block backfilling is enabled in innerVM: %w", err)
	}

	return vm.nextBlockBackfillData(ctx, innerBlkHeight)
}

func (vm *VM) BackfillBlocks(ctx context.Context, blksBytes [][]byte) (ids.ID, uint64, error) {
	blks := make(map[uint64]Block)

	// 1. Parse
	for i, blkBytes := range blksBytes {
		blk, err := vm.parseBlock(ctx, blkBytes)
		if err != nil {
			return ids.Empty, 0, fmt.Errorf("failed parsing backfilled block, index %d, %w", i, err)
		}
		blks[blk.Height()] = blk
	}

	// 2. Validate blocks, checking that they are continguous
	blkHeights := maps.Keys(blks)
	sort.Slice(blkHeights, func(i, j int) bool {
		return blkHeights[i] < blkHeights[j] // sort in ascending order by heights
	})

	var (
		topBlk = blks[blkHeights[len(blkHeights)-1]]
		topIdx = len(blkHeights) - 2
	)

	// vm.latestBackfilledBlock is non nil only if proposerVM has forked
	if vm.latestBackfilledBlock != ids.Empty {
		latestBackfilledBlk, err := vm.getBlock(ctx, vm.latestBackfilledBlock)
		if err != nil {
			return ids.Empty, 0, fmt.Errorf("failed retrieving latest backfilled block, %s, %w", vm.latestBackfilledBlock, err)
		}

		topBlk = latestBackfilledBlk
		topIdx = len(blkHeights) - 1
	}

	for i := topIdx; i >= 0; i-- {
		blk := blks[blkHeights[i]]
		if topBlk.Parent() != blk.ID() {
			return ids.Empty, 0, fmt.Errorf("unexpected backfilled block %s, expected child' parent is %s", blk.ID(), topBlk.Parent())
		}
		if err := blk.acceptOuterBlk(); err != nil {
			return ids.Empty, 0, fmt.Errorf("failed indexing backfilled block, blkID %s, %w", blk.ID(), err)
		}
		topBlk = blk
	}

	// 3. Backfill inner blocks to innerVM
	innerBlksBytes := make([][]byte, 0, len(blksBytes))
	for _, blk := range blks {
		innerBlksBytes = append(innerBlksBytes, blk.getInnerBlk().Bytes())
	}
	_, nextInnerBlkHeight, err := vm.ssVM.BackfillBlocks(ctx, innerBlksBytes)
	switch err {
	case block.ErrStopBlockBackfilling:
		return ids.Empty, 0, err // done backfilling
	case nil:
		// check alignment
	default:
		return ids.Empty, 0, fmt.Errorf("failed inner VM block backfilling, %w", err)
	}

	// 4. Check alignment
	for _, blk := range blks {
		innerBlkID := blk.getInnerBlk().ID()
		switch _, err := vm.ChainVM.GetBlock(ctx, innerBlkID); err {
		case nil:
			continue
		case database.ErrNotFound:
			if err := vm.revertBackfilledBlock(blk); err != nil {
				return ids.Empty, 0, fmt.Errorf("failed reverting backfilled VM block from height index %s, %w", blk.ID(), err)
			}
		default:
			return ids.Empty, 0, fmt.Errorf("failed checking innerVM block %s, %w", innerBlkID, err)
		}
	}

	return vm.nextBlockBackfillData(ctx, nextInnerBlkHeight)
}

func (vm *VM) nextBlockBackfillData(ctx context.Context, innerBlkHeight uint64) (ids.ID, uint64, error) {
	childBlkHeight := innerBlkHeight + 1
	childBlkID, err := vm.GetBlockIDAtHeight(ctx, childBlkHeight)
	if err != nil {
		return ids.Empty, 0, fmt.Errorf("failed retrieving proposer block ID at height %d: %w", childBlkHeight, err)
	}

	var childBlk snowman.Block
	childBlk, err = vm.getPostForkBlock(ctx, childBlkID)
	switch err {
	case nil:
		vm.latestBackfilledBlock = childBlkID
		if err := vm.State.SetLastBackfilledBlkID(childBlkID); err != nil {
			return ids.Empty, 0, fmt.Errorf("failed storing last backfilled block ID, %w", err)
		}
		if err := vm.db.Commit(); err != nil {
			return ids.Empty, 0, fmt.Errorf("failed committing backfilled blocks reversal, %w", err)
		}
	case database.ErrNotFound:
		// proposerVM may not be active yet.
		childBlk, err = vm.getPreForkBlock(ctx, childBlkID)
		if err != nil {
			return ids.Empty, 0, fmt.Errorf("failed retrieving innerVM block %s: %w", childBlkID, err)
		}
	default:
		return ids.Empty, 0, fmt.Errorf("failed retrieving proposer block %s: %w", childBlkID, err)
	}

	return childBlk.Parent(), childBlk.Height() - 1, nil
}

func (vm *VM) revertBackfilledBlock(blk Block) error {
	if err := vm.State.DeleteBlock(blk.ID()); err != nil {
		return fmt.Errorf("failed reverting backfilled VM block %s, %w", blk.ID(), err)
	}
	if err := vm.State.DeleteBlockIDAtHeight(blk.Height()); err != nil {
		return fmt.Errorf("failed reverting backfilled VM block from height index %s, %w", blk.ID(), err)
	}
	return nil
}
