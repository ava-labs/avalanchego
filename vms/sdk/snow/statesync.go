// Copyright (C) 2024, Ava Labs, Inv. All rights reserved.
// See the file LICENSE for licensing terms.

package snow

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/set"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/vms/sdk/event"
)

var _ block.StateSyncableVM = (*VM[ConcreteBlock, ConcreteBlock, ConcreteBlock])(nil)

func (v *VM[I, O, A]) SetStateSyncableVM(stateSyncableVM block.StateSyncableVM) {
	v.stateSyncableVM = stateSyncableVM
}

// StartStateSync notifies the VM to enter DynamicStateSync mode.
// The caller is responsible to eventually call FinishStateSync with a fully populated
// last accepted state.
func (v *VM[I, O, A]) StartStateSync(ctx context.Context, block I) error {
	if err := v.inputChainIndex.UpdateLastAccepted(ctx, block); err != nil {
		return err
	}
	v.ready = false
	v.setLastAccepted(NewInputBlock(v, block))
	return nil
}

// FinishStateSync completes dynamic state sync mode and sets the last accepted block to
// the given input/output/accepted value.
func (v *VM[I, O, A]) FinishStateSync(ctx context.Context, input I, output O, accepted A) error {
	v.chainLock.Lock()
	defer v.chainLock.Unlock()

	// Cannot call FinishStateSync if already marked as ready and in normal operation
	if v.ready {
		return fmt.Errorf("can't finish dynamic state sync from normal operation: %s", input)
	}

	// If the block is already the last accepted block, update the fields and return
	if input.GetID() == v.lastAcceptedBlock.GetID() {
		v.lastAcceptedBlock.setAccepted(output, accepted)
		v.log.Info("Finishing state sync with original target", zap.Stringer("lastAcceptedBlock", v.lastAcceptedBlock))
	} else {
		v.log.Info("Finishing state sync with target behind last accepted tip",
			zap.Stringer("target", input),
			zap.Stringer("lastAcceptedBlock", v.lastAcceptedBlock.Input),
		)
		start := time.Now()
		// Dynamic state sync notifies completion async, so the engine may continue to process/accept new blocks
		// before we grab chainLock.
		// This means we must reprocess blocks from the target state sync finished on to the updated last
		// accepted block.
		updatedLastAccepted, err := v.reprocessFromOutputToInput(ctx, v.lastAcceptedBlock.Input, output, accepted)
		if err != nil {
			return fmt.Errorf("failed to finish state sync while reprocessing to last accepted tip: %w", err)
		}
		v.setLastAccepted(updatedLastAccepted)
		v.log.Info("Finished reprocessing blocks", zap.Duration("duration", time.Since(start)))
	}

	if err := v.verifyProcessingBlocks(ctx); err != nil {
		return err
	}

	v.ready = true
	return nil
}

func (v *VM[I, O, A]) verifyProcessingBlocks(ctx context.Context) error {
	// Sort processing blocks by height
	v.verifiedL.Lock()
	v.log.Info("Verifying processing blocks after state sync", zap.Int("numBlocks", len(v.verifiedBlocks)))
	processingBlocks := make([]*Block[I, O, A], 0, len(v.verifiedBlocks))
	for _, blk := range v.verifiedBlocks {
		processingBlocks = append(processingBlocks, blk)
	}
	v.verifiedL.Unlock()
	slices.SortFunc(processingBlocks, func(a *Block[I, O, A], b *Block[I, O, A]) int {
		switch {
		case a.Height() < b.Height():
			return -1
		case a.Height() == b.Height():
			return 0
		default:
			return 1
		}
	})

	// Verify each block in order. An error here is not fatal because we may have vacuously verified blocks.
	// Therefore, if a block's parent has not already been verified, it invalidates all subsequent children
	// and we can safely drop the error here.
	invalidBlkIDs := set.NewSet[ids.ID](0)
	for _, blk := range processingBlocks {
		parent, err := v.GetBlock(ctx, blk.Parent())
		if err != nil {
			return fmt.Errorf("failed to fetch parent block %s while verifying processing block %s after state sync: %w", blk.Parent(), blk, err)
		}
		// the parent failed verification and this block is transitively invalid,
		// we are marking this block as unresolved
		if !parent.verified {
			v.log.Warn("Parent block not verified, skipping verification of processing block",
				zap.Stringer("parent", parent),
				zap.Stringer("block", blk),
			)
			invalidBlkIDs.Add(blk.ID())
			continue
		}
		if err := blk.verify(ctx, parent.Output); err != nil {
			invalidBlkIDs.Add(blk.ID())
			v.log.Warn("Failed to verify processing block after state sync", zap.Stringer("block", blk), zap.Error(err))
		}
	}

	unresolvedBlkCheck := newUnresolvedBlocksHealthCheck[I](invalidBlkIDs)
	v.AddPreRejectedSub(event.SubscriptionFunc[I]{
		NotifyF: func(_ context.Context, input I) error {
			unresolvedBlkCheck.Resolve(input.GetID())
			return nil
		},
	})
	if err := v.RegisterHealthChecker(unresolvedBlocksHealthChecker, unresolvedBlkCheck); err != nil {
		return err
	}

	return nil
}

func (v *VM[I, O, A]) StateSyncEnabled(ctx context.Context) (bool, error) {
	return v.stateSyncableVM.StateSyncEnabled(ctx)
}

func (v *VM[I, O, A]) GetOngoingSyncStateSummary(ctx context.Context) (block.StateSummary, error) {
	return v.stateSyncableVM.GetOngoingSyncStateSummary(ctx)
}

func (v *VM[I, O, A]) GetLastStateSummary(ctx context.Context) (block.StateSummary, error) {
	return v.stateSyncableVM.GetLastStateSummary(ctx)
}

func (v *VM[I, O, A]) ParseStateSummary(ctx context.Context, summaryBytes []byte) (block.StateSummary, error) {
	return v.stateSyncableVM.ParseStateSummary(ctx, summaryBytes)
}

func (v *VM[I, O, A]) GetStateSummary(ctx context.Context, summaryHeight uint64) (block.StateSummary, error) {
	return v.stateSyncableVM.GetStateSummary(ctx, summaryHeight)
}
