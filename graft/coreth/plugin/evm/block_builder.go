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
	// AP3 Paramters
	minBlockTime = 2 * time.Second
	maxBlockTime = 3 * time.Second
	batchSize    = 250

	// AP4 Parameters
	//
	// [buildRetryTime] is the frequency the node will attempt to build a block
	// (even if no new transaction has been received).
	//
	// NOTE: Because build block retries aren't activated until AP4, we don't need
	// to worry about putting nodes in a busy loop building blocks (in AP4 the engine
	// will only call build block during a node's proposer window). Nodes that are currently
	// designated to propose should frequently try to create a valid block during their window.
	buildRetryTime = 1 * time.Second

	dontBuild        buildingBlkStatus = iota
	conditionalBuild                   // Only used prior to AP4
	mayBuild                           // Only used prior to AP4
	building
)

type blockBuilder struct {
	ctx         *snow.Context
	chainConfig *params.ChainConfig

	chain   *coreth.ETHChain
	mempool *Mempool
	network *network

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
	//
	// NOTE: Only used prior to AP4.
	buildBlockTimer *timer.Timer

	// buildStatus signals the phase of block building the VM is currently in.
	// [dontBuild] indicates there's no need to build a block.
	// [conditionalBuild] indicates build a block if the batch size has been reached.
	// [mayBuild] indicates the VM should proceed to build a block.
	// [building] indicates the VM has sent a request to the engine to build a block.
	buildStatus buildingBlkStatus
}

func (vm *VM) NewBlockBuilder(notifyBuildBlockChan chan<- commonEng.Message) *blockBuilder {
	return &blockBuilder{
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
}

func (b *blockBuilder) handleBlockBuilding() {
	if !b.chainConfig.IsApricotPhase4(big.NewInt(time.Now().Unix())) {
		b.buildBlockTimer = timer.NewStagedTimer(b.buildBlockTwoStageTimer)
		go b.ctx.Log.RecoverAndPanic(b.buildBlockTimer.Dispatch)

		// Stop [buildBlockTwoStageTimer] at the start of AP4.
		b.shutdownWg.Add(1)
		go b.ctx.Log.RecoverAndPanic(b.stopBlockTimer)
	}
}

func (b *blockBuilder) stopBlockTimer() {
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
		b.buildBlockTimer.Stop()

		b.buildBlockLock.Lock()
		b.buildBlockTimer = nil
		// Flush any invalid statuses leftover from legacy block timer builder
		b.buildStatus = dontBuild
		b.buildBlockLock.Unlock()
	case <-b.shutdownChan:
		// buildBlockTimer will never be nil because we exit as soon as it is ever
		// set to nil.
		b.buildBlockTimer.Stop()
	}
}

func (b *blockBuilder) buildBlock() {
	b.buildBlockLock.Lock()
	defer b.buildBlockLock.Unlock()

	timestamp := big.NewInt(time.Now().Unix())
	if !b.chainConfig.IsApricotPhase4(timestamp) {
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
		// We will periodically retry block building in case a block cannot be
		// built now but could be built after the [baseFee] and/or
		// [blockGasCost] decrease.
		b.buildStatus = dontBuild
	}
}

// needToBuild returns true if there are outstanding transactions to be issued
// into a block.
//
// NOTE: Only used prior to AP4.
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
//
// NOTE: Only used prior to AP4.
func (b *blockBuilder) buildBlockTwoStageTimer() (time.Duration, bool) {
	b.buildBlockLock.Lock()
	defer b.buildBlockLock.Unlock()

	switch b.buildStatus {
	case dontBuild:
		return 0, false
	case conditionalBuild:
		if !b.buildEarly() {
			b.buildStatus = mayBuild
			return (maxBlockTime - minBlockTime), true
		}
	case mayBuild:
	case building:
		// If the status has already been set to building, there is no need
		// to send an additional request to the consensus engine until the call
		// to BuildBlock resets the block status.
		return 0, false
	default:
		// Log an error if an invalid status is found.
		log.Error("Found invalid build status in build block timer", "buildStatus", b.buildStatus)
	}

	select {
	case b.notifyBuildBlockChan <- commonEng.PendingTxs:
		b.buildStatus = building
	default:
		log.Error("Failed to push PendingTxs notification to the consensus engine.")
	}

	// No need for the timeout to fire again until BuildBlock is called.
	return 0, false
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

	timestamp := big.NewInt(time.Now().Unix())
	if !b.chainConfig.IsApricotPhase4(timestamp) {
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
	select {
	case b.notifyBuildBlockChan <- commonEng.PendingTxs:
		b.buildStatus = building
	default:
		log.Error("Failed to push PendingTxs notification to the consensus engine.")
	}
}

// retryBlockBuild triggers the rebuilding of a block if the [buildStatus] is
// [dontBuild] but there are pending transactions. This may lead to a block
// being built when it otherwise may not have due to a drop in [baseFee] or the
// [blockGasCost].
//
// NOTE: Only used after AP4.
func (b *blockBuilder) retryBlockBuild() {
	b.buildBlockLock.Lock()
	defer b.buildBlockLock.Unlock()

	if b.buildStatus != dontBuild {
		return
	}

	timestamp := big.NewInt(time.Now().Unix())
	if !b.chainConfig.IsApricotPhase4(timestamp) {
		return
	}

	if !b.needToBuild() {
		return
	}

	log.Trace("Retrying block building")
	select {
	case b.notifyBuildBlockChan <- commonEng.PendingTxs:
		b.buildStatus = building
	default:
		log.Error("Failed to push PendingTxs notification to the consensus engine.")
	}
}

// awaitSubmittedTxs waits for new transactions to be submitted
// and notifies the VM when the tx pool has transactions to be
// put into a new block.
func (b *blockBuilder) awaitSubmittedTxs() {
	b.shutdownWg.Add(1)
	go b.ctx.Log.RecoverAndPanic(func() {
		defer b.shutdownWg.Done()

		// txSubmitChan is invoked on reorgs
		txSubmitChan := b.chain.GetTxSubmitCh()

		// buildRetrier periodically triggers the engine to rebuild a block
		// although it may not have received any new transactions.
		buildRetrier := time.NewTicker(buildRetryTime)
		for {
			select {
			case ethTxsEvent := <-txSubmitChan:
				log.Trace("New tx detected, trying to generate a block")
				b.signalTxsReady()

				if b.network != nil {
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
				// Unlike EthTxs, AtomicTxs are gossiped in [issueTx] when they are
				// successfully added to the mempool.
			case <-buildRetrier.C:
				b.retryBlockBuild()
			case <-b.shutdownChan:
				return
			}
		}
	})
}
