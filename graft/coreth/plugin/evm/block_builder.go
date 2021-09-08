package evm

import (
	"math/big"
	"sync"
	"time"

	coreth "github.com/ava-labs/coreth/chain"
	"github.com/ava-labs/coreth/params"

	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ethereum/go-ethereum/log"
)

// buildingBlkStatus denotes the current status of the VM in block production.
type buildingBlkStatus uint8

const (
	minBlockTime = 2 * time.Second
	maxBlockTime = 3 * time.Second
	batchSize    = 250

	dontBuild buildingBlkStatus = iota
	conditionalBuild
	mayBuild
	building
)

type blockBuilder struct {
	chainConfig  *params.ChainConfig
	shutdownChan <-chan struct{}
	shutdownWg   *sync.WaitGroup

	chain   *coreth.ETHChain
	mempool *Mempool

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
}

func (vm *VM) NewBlockBuilder(notifyBuildBlockChan chan<- commonEng.Message) *blockBuilder {
	return &blockBuilder{
		chainConfig:          vm.chainConfig,
		chain:                vm.chain,
		mempool:              vm.mempool,
		shutdownChan:         vm.shutdownChan,
		shutdownWg:           &vm.shutdownWg,
		notifyBuildBlockChan: notifyBuildBlockChan,
		buildStatus:          dontBuild,
	}
}

func (b *blockBuilder) handleBlockBuilding() {
	if !b.chainConfig.IsApricotPhase4(big.NewInt(time.Now().Unix())) {
		// TODO: need to stop this timer after AP4 activation
		b.buildBlockTimer = timer.NewStagedTimer(b.buildBlockTwoStageTimer)

		// TODO: handle recover and panic
		go ctx.Log.RecoverAndPanic(vm.buildBlockTimer.Dispatch)

		// TODO: goroutine to stop buildBlockTimer at activation
		b.shutdownWg.Add(1)
		go func() {
			defer b.shutdownWg.Done()
			timestamp := time.Unix(b.chainConfig.ApricotPhase4BlockTimestamp.Int64(), 0)
			duration := time.Until(timestamp)
			select {
			case <-time.After(duration):
				b.buildBlockTimer.Stop()
				b.buildBlockTimer = nil
				// TODO: get lock and signal ready to build to flush any bad statuses
			case <-b.shutdownChan:
				if b.buildBlockTimer != nil {
					b.buildBlockTimer.Stop()
				}
			}
		}()
	}
}

// Set the buildStatus before calling Cancel or Issue on
// the mempool and after generating the block.
// This prevents [needToBuild] from returning true when the
// produced block will change whether or not we need to produce
// another block and also ensures that when the mempool adds a
// new item to Pending it will be handled appropriately by [signalTxsReady]
func (b *blockBuilder) buildBlock() {
	b.buildBlockLock.Lock()
	defer b.buildBlockLock.Unlock()

	timestamp := big.NewInt(time.Now().Unix())
	if !b.chainConfig.IsApricotPhase4(timestamp) {
		if b.needToBuild() {
			b.buildStatus = conditionalBuild
			b.buildBlockTimer.SetTimeoutIn(minBlockTime)
		} else {
			b.buildStatus = dontBuild
		}
	} else {
		// TODO: DON'T SET TO RETRY IN AP4 as it could cause more contention
		b.buildStatus = dontBuild
	}
}

// needToBuild returns true if there are outstanding transactions to be issued
// into a block.
func (b *blockBuilder) needToBuild() bool {
	// TODO: can modify to be sufficient tip in pool to pay fee...may not even
	// need?
	size, err := b.chain.PendingSize()
	if err != nil {
		log.Error("Failed to get chain pending size", "error", err)
		return false
	}
	return size > 0 || b.mempool.Len() > 0
}

// buildEarly returns true if there are sufficient outstanding transactions to
// be issued into a block to build a block early.
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

	timestamp := big.NewInt(time.Now().Unix())
	if !b.chainConfig.IsApricotPhase4(timestamp) {
		// Set the build block timer in motion if it has not been started.
		if b.buildStatus == dontBuild {
			b.buildStatus = conditionalBuild
			b.buildBlockTimer.SetTimeoutIn(minBlockTime)
		}
		return
	}

	// Check to see if mempool could pay effective tip? Or just yolo build and
	// leave all validation to build block time (could be some time)?
	// -> don't re-signal if already signaled
	if b.buildStatus == dontBuild {
		select {
		case b.notifyBuildBlockChan <- commonEng.PendingTxs:
			b.buildStatus = building
		default:
			log.Error("Failed to push PendingTxs notification to the consensus engine.")
		}
	}
}

// awaitSubmittedTxs waits for new transactions to be submitted
// and notifies the VM when the tx pool has transactions to be
// put into a new block.
func (b *blockBuilder) awaitSubmittedTxs() {
	b.shutdownWg.Add(1)
	go func() {
		// TODO: entry point for signaling ready to build
		defer b.shutdownWg.Done()
		// TODO: does this get called on reorg->yes?
		txSubmitChan := b.chain.GetTxSubmitCh()
		for {
			select {
			// TODO: check if pool has sufficient tip, if yes than send notify block
			// channel
			// 	case vm.notifyBuildBlockChan <- commonEng.PendingTxs:
			case <-txSubmitChan:
				log.Trace("New tx detected, trying to generate a block")
				b.signalTxsReady()
			case <-b.mempool.Pending:
				log.Trace("New atomic Tx detected, trying to generate a block")
				b.signalTxsReady()
			case <-b.shutdownChan:
				return
			}
		}
	}()
}
