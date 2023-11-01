// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
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
	if vm.ssVM == nil || !vm.stateSyncDone.Get() {
		return ids.Empty, 0, block.ErrBlockBackfillingNotEnabled
	}

	_, innerBlkHeight, err := vm.ssVM.BackfillBlocksEnabled(ctx)
	if err != nil {
		return ids.Empty, 0, fmt.Errorf("failed checking that block backfilling is enabled in innerVM: %w", err)
	}

	return vm.nextBlockBackfillData(ctx, innerBlkHeight)
}

func (vm *VM) BackfillBlocks(ctx context.Context, blksBytes [][]byte) (ids.ID, uint64, error) {
	// if vm implements Snowman++, a block height index must be available
	// to support state sync
	if err := vm.VerifyHeightIndex(ctx); err != nil {
		return ids.Empty, 0, fmt.Errorf("could not verify height index: %w", err)
	}

	var (
		blks           = make([]Block, 0, len(blksBytes))
		innerBlksBytes = make([][]byte, 0, len(blksBytes))
	)

	for i, blkBytes := range blksBytes {
		blk, err := vm.parseBlock(ctx, blkBytes)
		if err != nil {
			return ids.Empty, 0, fmt.Errorf("failed parsing backfilled block, index %d, %w", i, err)
		}

		// TODO: introduce validation

		blks = append(blks, blk)
		innerBlksBytes = append(innerBlksBytes, blk.getInnerBlk().Bytes())
	}

	_, nextInnerBlkHeight, err := vm.ssVM.BackfillBlocks(ctx, innerBlksBytes)
	if err == nil {
		// no error signals at least a single block has been accepted by the innerVM. (not necessarily all of them)
		// Find out which blocks have been accepted and store them.
		// Should the process err while looping there will be innerVM blocks not indexed by proposerVM. Repair will be
		// carried out upon restart.
		for _, blk := range blks {
			_, err := vm.ChainVM.GetBlock(ctx, blk.getInnerBlk().ID())
			if err != nil {
				continue
			}
			if err := blk.acceptOuterBlk(); err != nil {
				return ids.Empty, 0, fmt.Errorf("failed indexing backfilled block, blkID %s, %w", blk.ID(), err)
			}
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

	childBlk, err := vm.getBlock(ctx, childBlkID)
	if err != nil {
		return ids.Empty, 0, fmt.Errorf("failed retrieving proposer block %s: %w", childBlkID, err)
	}

	return childBlk.Parent(), innerBlkHeight, nil
}
