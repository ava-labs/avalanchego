// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metadata

import (
	"context"
	"sync"
	"time"

	"github.com/ava-labs/simplex/common"
	"go.uber.org/zap"
)

// blockBuildingDecision represents the decision of whether we should build a block at the current time,
// and if so, whether we should also transition to a new epoch along the way.
type blockBuildingDecision struct {
	buildInnerBlock bool
	transitionEpoch bool
	pChainHeight    uint64
}

// PChainProgressListener listens for changes in the P-chain height.
type PChainProgressListener interface {
	// WaitForProgress should block until either the context is cancelled, or the P-chain height has increased from the provided pChainHeight.
	WaitForProgress(ctx context.Context, pChainHeight uint64) error
}

type blockBuildingDecider struct {
	logger                   common.Logger
	maxBlockBuildingWaitTime time.Duration
	pChainListener           PChainProgressListener
	waitForPendingBlock      func(ctx context.Context)
	// hasValidatorSetChanged should return whether the validator set has changed since the
	// P-chain height referenced by the last block in the chain and until the provided P-chain height.
	hasValidatorSetChanged func(pChainHeight uint64) (bool, error)
	getPChainHeight        func() uint64
}

// shouldBuildBlock determines whether we should build a block at the current time,
// based on the current P-chain height and whether we should transition to a new epoch.
// It returns a blockBuildingDecision, the P-chain height sampled at the time of deciding,
// and an error if the decision cannot be made.
// The P-chain height is returned because sampling the P-chain height afterwards might be inconsistent with the decision that was made.
func (bbd *blockBuildingDecider) shouldBuildBlock(
	ctx context.Context,
) (blockBuildingDecision, error) {
	for {
		pChainHeight := bbd.getPChainHeight()

		shouldTransitionEpoch, err := bbd.hasValidatorSetChanged(pChainHeight)
		if err != nil {
			return blockBuildingDecision{}, err
		}

		if shouldTransitionEpoch {
			// If we should transition to a new epoch, maybe we can also build a block along the way.
			return bbd.buildBlockWithEpochTransition(ctx, pChainHeight)
		}

		// Else, we don't need to transition to a new epoch, but maybe we should build a block.
		// We wait for either the P-chain height to change, or for a block to be ready to be built.
		bbd.waitForPChainChangeOrPendingBlock(ctx, pChainHeight)

		// If the context was cancelled in the meantime, abandon evaluation.
		if ctx.Err() != nil {
			return blockBuildingDecision{}, ctx.Err()
		}

		// If we've reached here, either the P-chain height has changed, or a block is ready to be built.

		// If the P-chain height changed, re-evaluate again whether we should transition to a new epoch,
		// or continue waiting to build a block.
		if bbd.getPChainHeight() != pChainHeight {
			continue
		}

		// Else, we have reached here because a block is ready to be built, and the P-chain height has not changed,
		// which means we should build a block.

		return blockBuildingDecision{buildInnerBlock: true, pChainHeight: pChainHeight}, nil
	}
}

// waitForPChainChangeOrPendingBlock waits until either the given P-chain height changes from the provided pChainHeight,
// or a block is ready to be built, or if `ctx` gets cancelled.
func (bbd *blockBuildingDecider) waitForPChainChangeOrPendingBlock(ctx context.Context, pChainHeight uint64) {
	pChainAwareContext, cancel := context.WithCancel(ctx)

	var wg sync.WaitGroup
	wg.Add(1)

	defer wg.Wait()
	defer cancel()

	go func() {
		defer wg.Done()
		err := bbd.pChainListener.WaitForProgress(pChainAwareContext, pChainHeight)
		if err != nil && pChainAwareContext.Err() == nil {
			bbd.logger.Warn("error while waiting for P-chain progress", zap.Error(err))
		}
		cancel()
	}()

	bbd.waitForPendingBlock(pChainAwareContext)
}

// buildBlockWithEpochTransition decides if we should build a block while transitioning to a new epoch.
// It waits up to a limited amount of time (bbd.maxBlockBuildingWaitTime) for a block to be ready to be built,
// and if no block is ready by then, it returns the decision to transition epoch without building a block.
// Otherwise, it returns the decision to build a block and transition epoch along the way.
func (bbd *blockBuildingDecider) buildBlockWithEpochTransition(ctx context.Context, pChainHeight uint64) (blockBuildingDecision, error) {
	impatientContext, cancel := context.WithTimeout(ctx, bbd.maxBlockBuildingWaitTime)
	defer cancel()

	// We should transition to a new epoch, so we wait some time just in case we can also build a block along the way.
	// waitForPendingBlock will return in case a block is ready to be built, or when the context times out.
	bbd.waitForPendingBlock(impatientContext)

	if ctx.Err() != nil {
		return blockBuildingDecision{}, ctx.Err()
	}

	if impatientContext.Err() != nil {
		// We have returned from waitForPendingBlock because impatientContext has timed out,
		// which means we don't need to build a block.
		return blockBuildingDecision{transitionEpoch: true, pChainHeight: pChainHeight}, nil
	}

	// Block is ready to be built
	return blockBuildingDecision{buildInnerBlock: true, transitionEpoch: true, pChainHeight: pChainHeight}, nil
}
