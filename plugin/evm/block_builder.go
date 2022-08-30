// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"sync"
	"time"

	"github.com/ava-labs/coreth/params"

	"github.com/ava-labs/avalanchego/snow"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/coreth/core"
	"github.com/ethereum/go-ethereum/log"
)

// buildingBlkStatus denotes the current status of the VM in block production.
type buildingBlkStatus uint8

var (
	// AP4 Params
	minBlockTimeAP4 = 500 * time.Millisecond
)

const (
	// waitBlockTime is the amount of time to wait for BuildBlock to be
	// called by the engine before deciding whether or not to gossip the
	// transaction that triggered the PendingTxs message to the engine.
	//
	// This is done to reduce contention in the network when there is no
	// preferred producer. If we did not wait here, we may gossip a new
	// transaction to a peer while building a block that will conflict with
	// whatever the peer makes.
	waitBlockTime = 100 * time.Millisecond

	dontBuild buildingBlkStatus = iota
	mayBuild
	building
)

type blockBuilder struct {
	ctx         *snow.Context
	chainConfig *params.ChainConfig

	txPool   *core.TxPool
	mempool  *Mempool
	gossiper Gossiper

	shutdownChan <-chan struct{}
	shutdownWg   *sync.WaitGroup

	// A message is sent on this channel when a new block
	// is ready to be build. This notifies the consensus engine.
	notifyBuildBlockChan chan<- commonEng.Message

	// [buildBlockLock] must be held when accessing [buildStatus]
	buildBlockLock sync.Mutex

	// [buildBlockTimer] is a timer handling block production.
	buildBlockTimer *timer.Timer

	// buildStatus signals the phase of block building the VM is currently in.
	// [dontBuild] indicates there's no need to build a block.
	// [mayBuild] indicates the VM should proceed to build a block.
	// [building] indicates the VM has sent a request to the engine to build a block.
	buildStatus buildingBlkStatus
}

func (vm *VM) NewBlockBuilder(notifyBuildBlockChan chan<- commonEng.Message) *blockBuilder {
	b := &blockBuilder{
		ctx:                  vm.ctx,
		chainConfig:          vm.chainConfig,
		txPool:               vm.txPool,
		mempool:              vm.mempool,
		gossiper:             vm.gossiper,
		shutdownChan:         vm.shutdownChan,
		shutdownWg:           &vm.shutdownWg,
		notifyBuildBlockChan: notifyBuildBlockChan,
		buildStatus:          dontBuild,
	}

	b.handleBlockBuilding()
	return b
}

func (b *blockBuilder) handleBlockBuilding() {
	b.buildBlockTimer = timer.NewTimer(b.buildBlockTimerCallback)
	go b.ctx.Log.RecoverAndPanic(b.buildBlockTimer.Dispatch)
}

// handleGenerateBlock should be called immediately after [BuildBlock].
// [handleGenerateBlock] invocation could lead to quiesence, building a block with
// some delay, or attempting to build another block immediately.
func (b *blockBuilder) handleGenerateBlock() {
	b.buildBlockLock.Lock()
	defer b.buildBlockLock.Unlock()

	// If we still need to build a block immediately after building, we let the
	// engine know it [mayBuild] in [minBlockTimeAP4].
	//
	// It is often the case in AP4 that a block (with the same txs) could be built
	// after a few seconds of delay as the [baseFee] and/or [blockGasCost] decrease.
	if b.needToBuild() {
		b.buildStatus = mayBuild
		b.buildBlockTimer.SetTimeoutIn(minBlockTimeAP4)
	} else {
		b.buildStatus = dontBuild
	}
}

// needToBuild returns true if there are outstanding transactions to be issued
// into a block.
func (b *blockBuilder) needToBuild() bool {
	size := b.txPool.PendingSize()
	return size > 0 || b.mempool.Len() > 0
}

// buildBlockTimerCallback is the timer callback that sends a notification
// to the engine when the VM is ready to build a block.
func (b *blockBuilder) buildBlockTimerCallback() {
	b.buildBlockLock.Lock()
	defer b.buildBlockLock.Unlock()

	switch b.buildStatus {
	case dontBuild:
	case mayBuild:
		b.markBuilding()
	case building:
		// If the status has already been set to building, there is no need
		// to send an additional request to the consensus engine until the call
		// to BuildBlock resets the block status.
	default:
		// Log an error if an invalid status is found.
		log.Error("Found invalid build status in build block timer", "buildStatus", b.buildStatus)
	}
}

// markBuilding assumes the [buildBlockLock] is held.
func (b *blockBuilder) markBuilding() {
	select {
	case b.notifyBuildBlockChan <- commonEng.PendingTxs:
		b.buildStatus = building
	default:
		log.Error("Failed to push PendingTxs notification to the consensus engine.")
	}
}

// signalTxsReady notifies the engine and sets the status to [building] if the
// status is [dontBuild]. Otherwise, the attempt has already begun and this notification
// can be safely skipped.
func (b *blockBuilder) signalTxsReady() {
	b.buildBlockLock.Lock()
	defer b.buildBlockLock.Unlock()

	if b.buildStatus != dontBuild {
		return
	}

	// We take a naive approach here and signal the engine that we should build
	// a block as soon as we receive at least one transaction.
	//
	// In the future, we may wish to add optimization here to only signal the
	// engine if the sum of the projected tips in the mempool satisfies the
	// required block fee.
	b.markBuilding()
}

// awaitSubmittedTxs waits for new transactions to be submitted
// and notifies the VM when the tx pool has transactions to be
// put into a new block.
func (b *blockBuilder) awaitSubmittedTxs() {
	// txSubmitChan is invoked when new transactions are issued as well as on re-orgs which
	// may orphan transactions that were previously in a preferred block.
	txSubmitChan := make(chan core.NewTxsEvent)
	b.txPool.SubscribeNewTxsEvent(txSubmitChan)

	b.shutdownWg.Add(1)
	go b.ctx.Log.RecoverAndPanic(func() {
		defer b.shutdownWg.Done()

		for {
			select {
			case ethTxsEvent := <-txSubmitChan:
				log.Trace("New tx detected, trying to generate a block")
				b.signalTxsReady()

				if b.gossiper != nil && len(ethTxsEvent.Txs) > 0 {
					// Give time for this node to build a block before attempting to
					// gossip
					time.Sleep(waitBlockTime)
					// [GossipEthTxs] will block unless [gossiper.ethTxsToGossipChan] (an
					// unbuffered channel) is listened on
					if err := b.gossiper.GossipEthTxs(ethTxsEvent.Txs); err != nil {
						log.Warn(
							"failed to gossip new eth transactions",
							"err", err,
						)
					}
				}
			case <-b.mempool.Pending:
				log.Trace("New atomic Tx detected, trying to generate a block")
				b.signalTxsReady()

				newTxs := b.mempool.GetNewTxs()
				if b.gossiper != nil && len(newTxs) > 0 {
					// Give time for this node to build a block before attempting to
					// gossip
					time.Sleep(waitBlockTime)
					if err := b.gossiper.GossipAtomicTxs(newTxs); err != nil {
						log.Warn(
							"failed to gossip new atomic transactions",
							"err", err,
						)
					}
				}
			case <-b.shutdownChan:
				b.buildBlockTimer.Stop()
				return
			}
		}
	})
}
