// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/lock"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/log"
	"github.com/holiman/uint256"
	"go.uber.org/zap"

	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/core/txpool"
	"github.com/ava-labs/subnet-evm/plugin/evm/customtypes"

	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
)

const (
	// Minimum amount of time to wait to retry building a block after a failed attempt.
	// It is assumed that the first block building attempt is already after the minimum delay time.
	RetryDelay = 100 * time.Millisecond
)

type blockBuilder struct {
	clock *mockable.Clock
	ctx   *snow.Context

	txPool *txpool.TxPool

	shutdownChan <-chan struct{}
	shutdownWg   *sync.WaitGroup

	pendingSignal *lock.Cond

	buildBlockLock sync.Mutex
	// lastBuildParentHash is the parent hash of the last block that was built.
	// This and lastBuildTime are used to ensure that we don't build blocks too frequently,
	// but at least after a minimum delay of minBlockBuildingRetryDelay.
	lastBuildParentHash common.Hash
	lastBuildTime       time.Time

	chainHeadHash   common.Hash
	mempoolHeadHash common.Hash
}

func (vm *VM) NewBlockBuilder() *blockBuilder {
	b := &blockBuilder{
		ctx:          vm.ctx,
		txPool:       vm.txPool,
		shutdownChan: vm.shutdownChan,
		shutdownWg:   &vm.shutdownWg,
		clock:        vm.clock,
	}
	b.pendingSignal = lock.NewCond(&b.buildBlockLock)
	return b
}

// handleGenerateBlock is called from the VM immediately after BuildBlock.
func (b *blockBuilder) handleGenerateBlock(currentParentHash common.Hash) {
	b.buildBlockLock.Lock()
	defer b.buildBlockLock.Unlock()
	b.lastBuildTime = b.clock.Time()
	b.lastBuildParentHash = currentParentHash
}

// needToBuild returns true if there are outstanding transactions to be issued
// into a block.
func (b *blockBuilder) needToBuild() bool {
	size := b.txPool.PendingSize(txpool.PendingFilter{
		MinTip: uint256.MustFromBig(b.txPool.GasTip()),
	})
	return size > 0
}

// signalCanBuild signals that a new block can be built.
func (b *blockBuilder) signalCanBuild() {
	b.buildBlockLock.Lock()
	defer b.buildBlockLock.Unlock()
	b.pendingSignal.Broadcast()
}

// awaitSubmittedTxs waits for new transactions to be submitted
// and notifies the VM when the tx pool has transactions to be
// put into a new block.
func (b *blockBuilder) awaitSubmittedTxs() {
	// txSubmitChan is invoked when new transactions are issued as well as on re-orgs which
	// may orphan transactions that were previously in a preferred block.
	txSubmitChan := make(chan core.NewTxsEvent)
	b.txPool.SubscribeTransactions(txSubmitChan, true)

	events := make(chan core.NewTxPoolReorgEvent)
	sub := b.txPool.SubscribeNewReorgEvent(events)

	b.shutdownWg.Add(1)
	go b.ctx.Log.RecoverAndPanic(func() {
		defer b.shutdownWg.Done()
		defer sub.Unsubscribe()

		for {
			select {
			case <-txSubmitChan:
				log.Trace("New tx detected, trying to generate a block")
				b.signalCanBuild()
			case <-b.shutdownChan:
				return
			case event := <-events:
				if event.Head == nil || event.Head.Number == nil {
					log.Warn("nil head or block number in tx pool reorg event")
					continue
				}
				b.setMempoolHeadHash(event.Head.Hash())
			}
		}
	})
}

// waitForEvent waits until a block needs to be built.
// It returns only after at least [minBlockBuildingRetryDelay] passed from the last time a block was built.
func (b *blockBuilder) waitForEvent(ctx context.Context, currentHeader *types.Header) (commonEng.Message, error) {
	lastBuildTime, lastBuildParentHash, err := b.waitForNeedToBuild(ctx)
	if err != nil {
		return 0, err
	}
	timeUntilNextBuild := b.calculateBlockBuildingDelay(
		lastBuildTime,
		lastBuildParentHash,
		currentHeader,
	)
	if timeUntilNextBuild <= 0 {
		b.ctx.Log.Debug("Last time we built a block was long enough ago or this is not a retry, no need to wait")
		return commonEng.PendingTxs, nil
	}

	b.ctx.Log.Debug("Last time we built a block was too recent, waiting",
		zap.Duration("timeUntilNextBuild", timeUntilNextBuild),
	)
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-time.After(timeUntilNextBuild):
		return commonEng.PendingTxs, nil
	}
}

// waitForNeedToBuild waits until needToBuild returns true.
// It returns the last time a block was built.
func (b *blockBuilder) waitForNeedToBuild(ctx context.Context) (time.Time, common.Hash, error) {
	b.buildBlockLock.Lock()
	defer b.buildBlockLock.Unlock()
	for !b.needToBuild() || b.pendingPoolUpdate() {
		if err := b.pendingSignal.Wait(ctx); err != nil {
			return time.Time{}, common.Hash{}, err
		}
	}
	return b.lastBuildTime, b.lastBuildParentHash, nil
}

// calculateBlockBuildingDelay calculates the delay needed before building the next block.
func (b *blockBuilder) calculateBlockBuildingDelay(
	lastBuildTime time.Time,
	lastBuildParentHash common.Hash,
	currentHeader *types.Header,
) time.Duration {
	var nextBuildTime time.Time
	isRetry := lastBuildParentHash == currentHeader.ParentHash && !lastBuildTime.IsZero() // if last build time is zero, this is not a retry
	// If this is a retry, we already have waited for the minimum next block time in a previous attempt,
	// and only need to wait for the retry delay.
	if isRetry {
		nextBuildTime = lastBuildTime.Add(RetryDelay)
	} else {
		// If this is not a retry, we need to wait for the minimum next block time.
		nextBuildTime = minNextBlockTime(currentHeader)
	}
	remainingDelay := nextBuildTime.Sub(b.clock.Time())
	return max(remainingDelay, 0)
}

// minNextBlockTime calculates the minimum next block time based on the current header.
func minNextBlockTime(parent *types.Header) time.Time {
	parentExtra := customtypes.GetHeaderExtra(parent)
	// If the parent header has no min delay excess, there is nothing to wait for, because the rule does not apply
	// to the block to be built.
	if parentExtra.MinDelayExcess == nil {
		return time.Time{}
	}
	parentTime := customtypes.BlockTime(parent)
	acp226DelayExcess := *parentExtra.MinDelayExcess
	// parent's delay excess is already verified by consensus
	// so this should not overflow
	requiredDelay := time.Duration(acp226DelayExcess.Delay()) * time.Millisecond
	return parentTime.Add(requiredDelay)
}

func (b *blockBuilder) setChainHeadHash(hash common.Hash) {
	b.buildBlockLock.Lock()
	defer b.buildBlockLock.Unlock()

	b.chainHeadHash = hash

	if b.pendingPoolUpdate() {
		return
	}

	b.pendingSignal.Broadcast()
}

func (b *blockBuilder) setMempoolHeadHash(hash common.Hash) {
	b.buildBlockLock.Lock()
	defer b.buildBlockLock.Unlock()

	b.mempoolHeadHash = hash

	if b.pendingPoolUpdate() {
		return
	}

	b.pendingSignal.Broadcast()
}

func (b *blockBuilder) pendingPoolUpdate() bool {
	return b.chainHeadHash != b.mempoolHeadHash
}
