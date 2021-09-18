// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"math/big"
	"sync"
	"time"

	coreth "github.com/ava-labs/coreth/chain"
	"github.com/ava-labs/coreth/params"

	"github.com/ava-labs/avalanchego/snow"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ethereum/go-ethereum/log"
)

// buildingBlkStatus denotes the current status of the VM in block production.
type buildingBlkStatus uint8

const (
	// AP3 Parameters
	minBlockTime = 2 * time.Second
	maxBlockTime = 3 * time.Second
	batchSize    = 250

	// AP4 Parameters
	minBlockTimeAP4 = 500 * time.Millisecond
	// waitBlockTime is the amount of time to wait for BuildBlock to be
	// called by the engine before deciding whether or not to gossip the
	// transaction that triggered the PendingTxs message to the engine.
	//
	// This is done to reduce contention in the network when there is no
	// preferred producer. If we did not wait here, we may gossip a new
	// transaction to a peer while building a block that will conflict with
	// whatever the peer makes.
	waitBlockTime = 100 * time.Millisecond

	dontBuild        buildingBlkStatus = iota
	conditionalBuild                   // Only used prior to AP4
	mayBuild
	building
)

type blockBuilder struct {
	ctx         *snow.Context
	chainConfig *params.ChainConfig

	chain   *coreth.ETHChain
	mempool *Mempool
	network Network

	shutdownChan <-chan struct{}
	shutdownWg   *sync.WaitGroup

	// A message is sent on this channel when a new block
	// is ready to be build. This notifies the consensus engine.
	notifyBuildBlockChan chan<- commonEng.Message

	// [buildBlockLock] must be held when accessing [buildStatus]
	buildBlockLock sync.Mutex

	// [buildBlockTimer] is a two stage timer handling block production.
	// Stage1 build a block if the batch size has been reached.
	// Stage2 build a block regardless of the size.
	buildBlockTimer *timer.Timer

	// buildStatus signals the phase of block building the VM is currently in.
	// [dontBuild] indicates there's no need to build a block.
	// [conditionalBuild] indicates build a block if the batch size has been reached.
	// [mayBuild] indicates the VM should proceed to build a block.
	// [building] indicates the VM has sent a request to the engine to build a block.
	buildStatus buildingBlkStatus

	// isAP4 is a boolean indicating if AP4 is activated. This prevents us from
	// getting the current time and comparing it to the *params.chainConfig more
	// than once.
	isAP4 bool
}

func (vm *VM) NewBlockBuilder(notifyBuildBlockChan chan<- commonEng.Message) *blockBuilder {
	b := &blockBuilder{
		ctx:                  vm.ctx,
		chainConfig:          vm.chainConfig,
		chain:                vm.chain,
		mempool:              vm.mempool,
		network:              vm.network,
		shutdownChan:         vm.shutdownChan,
		shutdownWg:           &vm.shutdownWg,
		notifyBuildBlockChan: notifyBuildBlockChan,
		buildStatus:          dontBuild,
	}

	b.handleBlockBuilding()
	return b
}

func (b *blockBuilder) handleBlockBuilding() {
	b.buildBlockTimer = timer.NewStagedTimer(b.buildBlockTwoStageTimer)
	go b.ctx.Log.RecoverAndPanic(b.buildBlockTimer.Dispatch)

	if !b.chainConfig.IsApricotPhase4(big.NewInt(time.Now().Unix())) {
		b.shutdownWg.Add(1)
		go b.ctx.Log.RecoverAndPanic(b.migrateAP4)
	} else {
		b.isAP4 = true
	}
}

func (b *blockBuilder) migrateAP4() {
	defer b.shutdownWg.Done()

	// In some tests, the AP4 timestamp is not populated. If this is the case, we
	// should only stop [buildBlockTwoStageTimer] on shutdown.
	if b.chainConfig.ApricotPhase4BlockTimestamp == nil {
		<-b.shutdownChan
		b.buildBlockTimer.Stop()
		return
	}

	timestamp := time.Unix(b.chainConfig.ApricotPhase4BlockTimestamp.Int64(), 0)
	duration := time.Until(timestamp)
	select {
	case <-time.After(duration):
		b.isAP4 = true
		b.buildBlockLock.Lock()
		// Flush any invalid statuses leftover from legacy block timer builder
		if b.buildStatus == conditionalBuild {
			b.buildStatus = mayBuild
		}
		b.buildBlockLock.Unlock()
	case <-b.shutdownChan:
		// buildBlockTimer will never be nil because we exit as soon as it is ever
		// set to nil.
		b.buildBlockTimer.Stop()
	}
}

// handleGenerateBlock should be called immediately after [BuildBlock].
// [handleGenerateBlock] invocation could lead to quiesence, building a block with
// some delay, or attempting to build another block immediately.
func (b *blockBuilder) handleGenerateBlock() {
	b.buildBlockLock.Lock()
	defer b.buildBlockLock.Unlock()

	if !b.isAP4 {
		// Set the buildStatus before calling Cancel or Issue on
		// the mempool and after generating the block.
		// This prevents [needToBuild] from returning true when the
		// produced block will change whether or not we need to produce
		// another block and also ensures that when the mempool adds a
		// new item to Pending it will be handled appropriately by [signalTxsReady]
		if b.needToBuild() {
			b.buildStatus = conditionalBuild
			b.buildBlockTimer.SetTimeoutIn(minBlockTime)
		} else {
			b.buildStatus = dontBuild
		}
	} else {
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
}

// needToBuild returns true if there are outstanding transactions to be issued
// into a block.
func (b *blockBuilder) needToBuild() bool {
	size, err := b.chain.PendingSize()
	if err != nil {
		log.Error("Failed to get chain pending size", "error", err)
		return false
	}
	return size > 0 || b.mempool.Len() > 0
}

// buildEarly returns true if there are sufficient outstanding transactions to
// be issued into a block to build a block early.
//
// NOTE: Only used prior to AP4.
func (b *blockBuilder) buildEarly() bool {
	size, err := b.chain.PendingSize()
	if err != nil {
		log.Error("Failed to get chain pending size", "error", err)
		return false
	}
	return size > batchSize || b.mempool.Len() > 1
}

// buildBlockTwoStageTimer is a two stage timer that sends a notification
// to the engine when the VM is ready to build a block.
// If it should be called back again, it returns the timeout duration at
// which it should be called again.
func (b *blockBuilder) buildBlockTwoStageTimer() (time.Duration, bool) {
	b.buildBlockLock.Lock()
	defer b.buildBlockLock.Unlock()

	switch b.buildStatus {
	case dontBuild:
	case conditionalBuild:
		if !b.buildEarly() {
			b.buildStatus = mayBuild
			return (maxBlockTime - minBlockTime), true
		}
		b.markBuilding()
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

	// No need for the timeout to fire again until BuildBlock is called.
	return 0, false
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

// signalTxsReady sets the initial timeout on the two stage timer if the process
// has not already begun from an earlier notification. If [buildStatus] is anything
// other than [dontBuild], then the attempt has already begun and this notification
// can be safely skipped.
func (b *blockBuilder) signalTxsReady() {
	b.buildBlockLock.Lock()
	defer b.buildBlockLock.Unlock()

	if b.buildStatus != dontBuild {
		return
	}

	if !b.isAP4 {
		b.buildStatus = conditionalBuild
		b.buildBlockTimer.SetTimeoutIn(minBlockTime)
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
	b.shutdownWg.Add(1)
	go b.ctx.Log.RecoverAndPanic(func() {
		defer b.shutdownWg.Done()

		// txSubmitChan is invoked when new transactions are issued as well as on re-orgs which
		// may orphan transactions that were previously in a preferred block.
		txSubmitChan := b.chain.GetTxSubmitCh()
		for {
			select {
			case ethTxsEvent := <-txSubmitChan:
				log.Trace("New tx detected, trying to generate a block")
				b.signalTxsReady()

				// We only attempt to invoke [GossipEthTxs] once AP4 is activated
				if b.isAP4 && b.network != nil && len(ethTxsEvent.Txs) > 0 {
					// Give time for this node to build a block before attempting to
					// gossip
					time.Sleep(waitBlockTime)
					// [GossipEthTxs] will block unless [pushNetwork.ethTxsToGossipChan] (an
					// unbuffered channel) is listened on
					if err := b.network.GossipEthTxs(ethTxsEvent.Txs); err != nil {
						log.Warn(
							"failed to gossip new eth transactions",
							"err", err,
						)
					}
				}
			case <-b.mempool.Pending:
				log.Trace("New atomic Tx detected, trying to generate a block")
				b.signalTxsReady()

				// We only attempt to invoke [GossipAtomicTxs] once AP4 is activated
				newTxs := b.mempool.GetNewTxs()
				if b.isAP4 && b.network != nil && len(newTxs) > 0 {
					// Give time for this node to build a block before attempting to
					// gossip
					time.Sleep(waitBlockTime)
					if err := b.network.GossipAtomicTxs(newTxs); err != nil {
						log.Warn(
							"failed to gossip new atomic transactions",
							"err", err,
						)
					}
				}
			case <-b.shutdownChan:
				return
			}
		}
	})
}
