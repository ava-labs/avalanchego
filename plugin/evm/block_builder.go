// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/lock"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/log"
	"github.com/holiman/uint256"
	"go.uber.org/zap"

	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/core/txpool"

	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
)

const (
	// Minimum amount of time to wait after building a block before attempting to build a block
	// a second time without changing the contents of the mempool.
	minBlockBuildingRetryDelay = 500 * time.Millisecond
)

type blockBuilder struct {
	ctx *snow.Context

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
}

func (vm *VM) NewBlockBuilder() *blockBuilder {
	b := &blockBuilder{
		ctx:          vm.ctx,
		txPool:       vm.txPool,
		shutdownChan: vm.shutdownChan,
		shutdownWg:   &vm.shutdownWg,
	}
	b.pendingSignal = lock.NewCond(&b.buildBlockLock)
	return b
}

// handleGenerateBlock is called from the VM immediately after BuildBlock.
func (b *blockBuilder) handleGenerateBlock(currentParentHash common.Hash) {
	b.buildBlockLock.Lock()
	defer b.buildBlockLock.Unlock()
	b.lastBuildTime = time.Now()
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

	b.shutdownWg.Add(1)
	go b.ctx.Log.RecoverAndPanic(func() {
		defer b.shutdownWg.Done()

		for {
			select {
			case <-txSubmitChan:
				log.Trace("New tx detected, trying to generate a block")
				b.signalCanBuild()
			case <-b.shutdownChan:
				return
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
	timeSinceLastBuildTime := time.Since(lastBuildTime)
	isRetry := lastBuildParentHash == currentHeader.ParentHash
	// 1. if this is not a retry
	// 2. if this is the first time we try to build a block
	// 3. if the time since the last build is greater than the minimum retry delay
	// then we can build a block immediately.
	if !isRetry || lastBuildTime.IsZero() || timeSinceLastBuildTime >= minBlockBuildingRetryDelay {
		b.ctx.Log.Debug("Last time we built a block was long enough ago or this is not a retry, no need to wait",
			zap.Duration("timeSinceLastBuildTime", timeSinceLastBuildTime),
			zap.Bool("isRetry", isRetry),
		)
		return commonEng.PendingTxs, nil
	}
	timeUntilNextBuild := minBlockBuildingRetryDelay - timeSinceLastBuildTime
	log.Debug("Last time we built a block was too recent, waiting", "timeUntilNextBuild", timeUntilNextBuild)
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
	for !b.needToBuild() {
		if err := b.pendingSignal.Wait(ctx); err != nil {
			return time.Time{}, common.Hash{}, err
		}
	}
	return b.lastBuildTime, b.lastBuildParentHash, nil
}
