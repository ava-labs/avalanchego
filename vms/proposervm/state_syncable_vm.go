// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/summary"
)

// buildStateSummary only produces a post-fork summary (one carrying a proposervm
// block) on the path where forkHeight <= innerSummary.Height(), and it always
// embeds the block accepted at innerSummary.Height(). ParseStateSummary enforces
// both, so that a malformed summary from a state-sync beacon is discarded rather
// than accepted and later relied on:
//   - a block below the inner summary height leaves the proposervm behind the
//     inner VM, which repairAcceptedChainByHeight treats as fatal with no
//     automatic recovery;
//   - a fork height above the inner summary height is persisted above the
//     proposervm's own last accepted height, breaking the forkHeight <=
//     acceptedHeight invariant that GetBlockIDAtHeight routing and
//     updateHeightIndex (height - forkHeight, which then underflows on pruning
//     nodes) rely on.
var (
	errStateSummaryHeightMismatch = errors.New("state summary block height doesn't match inner summary height")
	errStateSummaryForkHeight     = errors.New("state summary fork height above inner summary height")
)

func (vm *VM) StateSyncEnabled(ctx context.Context) (bool, error) {
	if vm.ssVM == nil {
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
	block, err := vm.parsePostForkBlock(ctx, statelessSummary.BlockBytes(), true)
	if err != nil {
		return nil, fmt.Errorf("could not parse proposervm block bytes from summary due to: %w", err)
	}

	if block.Height() != innerSummary.Height() {
		return nil, fmt.Errorf("%w: block height %d, inner summary height %d",
			errStateSummaryHeightMismatch,
			block.Height(),
			innerSummary.Height(),
		)
	}

	if statelessSummary.ForkHeight() > innerSummary.Height() {
		return nil, fmt.Errorf("%w: fork height %d, inner summary height %d",
			errStateSummaryForkHeight,
			statelessSummary.ForkHeight(),
			innerSummary.Height(),
		)
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
