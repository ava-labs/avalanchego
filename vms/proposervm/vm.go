// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/proposervm/indexer"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
	"github.com/ava-labs/avalanchego/vms/proposervm/scheduler"
	"github.com/ava-labs/avalanchego/vms/proposervm/state"
	"github.com/ava-labs/avalanchego/vms/proposervm/tree"

	statelessblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

const (
	// minBlockDelay should be kept as whole seconds because block timestamps
	// are only specific to the second.
	minBlockDelay         = time.Second
	checkIndexedFrequency = 10 * time.Second
	innerBlkCacheSize     = 512
)

var (
	_ block.ChainVM              = (*VM)(nil)
	_ block.BatchedChainVM       = (*VM)(nil)
	_ block.HeightIndexedChainVM = (*VM)(nil)
	_ block.StateSyncableVM      = (*VM)(nil)

	dbPrefix = []byte("proposervm")
)

type VM struct {
	block.ChainVM
	bVM  block.BatchedChainVM
	hVM  block.HeightIndexedChainVM
	ssVM block.StateSyncableVM

	activationTime      time.Time
	minimumPChainHeight uint64

	state.State
	hIndexer                indexer.HeightIndexer
	resetHeightIndexOngoing utils.AtomicBool

	proposer.Windower
	tree.Tree
	scheduler.Scheduler
	mockable.Clock

	ctx         *snow.Context
	db          *versiondb.Database
	toScheduler chan<- common.Message

	// Block ID --> Block
	// Each element is a block that passed verification but
	// hasn't yet been accepted/rejected
	verifiedBlocks map[ids.ID]PostForkBlock
	// Stateless block ID --> inner block.
	// Only contains post-fork blocks near the tip so that the cache doesn't get
	// filled with random blocks every time this node parses blocks while
	// processing a GetAncestors message from a bootstrapping node.
	innerBlkCache  cache.Cacher
	preferred      ids.ID
	consensusState snow.State
	context        context.Context
	onShutdown     func()

	// lastAcceptedTime is set to the last accepted PostForkBlock's timestamp
	// if the last accepted block has been a PostForkOption block since having
	// initialized the VM.
	lastAcceptedTime time.Time

	// lastAcceptedHeight is set to the last accepted PostForkBlock's height.
	lastAcceptedHeight uint64
}

func New(
	vm block.ChainVM,
	activationTime time.Time,
	minimumPChainHeight uint64,
) *VM {
	bVM, _ := vm.(block.BatchedChainVM)
	hVM, _ := vm.(block.HeightIndexedChainVM)
	ssVM, _ := vm.(block.StateSyncableVM)
	return &VM{
		ChainVM: vm,
		bVM:     bVM,
		hVM:     hVM,
		ssVM:    ssVM,

		activationTime:      activationTime,
		minimumPChainHeight: minimumPChainHeight,
	}
}

func (vm *VM) Initialize(
	ctx *snow.Context,
	dbManager manager.Manager,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngine chan<- common.Message,
	fxs []*common.Fx,
	appSender common.AppSender,
) error {
	// TODO: Add a helper for this metrics override, it is performed in multiple
	//       places.
	multiGatherer := metrics.NewMultiGatherer()
	registerer := prometheus.NewRegistry()
	if err := multiGatherer.Register("proposervm", registerer); err != nil {
		return err
	}

	optionalGatherer := metrics.NewOptionalGatherer()
	if err := multiGatherer.Register("", optionalGatherer); err != nil {
		return err
	}
	if err := ctx.Metrics.Register(multiGatherer); err != nil {
		return err
	}
	ctx.Metrics = optionalGatherer

	vm.ctx = ctx
	rawDB := dbManager.Current().Database
	prefixDB := prefixdb.New(dbPrefix, rawDB)
	vm.db = versiondb.New(prefixDB)
	vm.State = state.New(vm.db)
	vm.Windower = proposer.New(ctx.ValidatorState, ctx.SubnetID, ctx.ChainID)
	vm.Tree = tree.New()
	innerBlkCache, err := metercacher.New(
		"inner_block_cache",
		registerer,
		&cache.LRU{Size: innerBlkCacheSize},
	)
	if err != nil {
		return err
	}
	vm.innerBlkCache = innerBlkCache

	indexerDB := versiondb.New(vm.db)
	// TODO: Use [state.NewMetered] here to populate additional metrics.
	indexerState := state.New(indexerDB)
	vm.hIndexer = indexer.NewHeightIndexer(vm, vm.ctx.Log, indexerState)

	scheduler, vmToEngine := scheduler.New(vm.ctx.Log, toEngine)
	vm.Scheduler = scheduler
	vm.toScheduler = vmToEngine

	go ctx.Log.RecoverAndPanic(func() {
		scheduler.Dispatch(time.Now())
	})

	vm.verifiedBlocks = make(map[ids.ID]PostForkBlock)
	context, cancel := context.WithCancel(context.Background())
	vm.context = context
	vm.onShutdown = cancel

	err = vm.ChainVM.Initialize(
		ctx,
		dbManager,
		genesisBytes,
		upgradeBytes,
		configBytes,
		vmToEngine,
		fxs,
		appSender,
	)
	if err != nil {
		return err
	}

	if err := vm.repair(indexerState); err != nil {
		return err
	}

	return vm.setLastAcceptedMetadata()
}

// shutdown ops then propagate shutdown to innerVM
func (vm *VM) Shutdown() error {
	vm.onShutdown()

	if err := vm.db.Commit(); err != nil {
		return err
	}
	return vm.ChainVM.Shutdown()
}

func (vm *VM) SetState(newState snow.State) error {
	if err := vm.ChainVM.SetState(newState); err != nil {
		return err
	}

	oldState := vm.consensusState
	vm.consensusState = newState
	if oldState != snow.StateSyncing {
		return nil
	}

	// When finishing StateSyncing, if state sync has failed or was skipped,
	// repairAcceptedChainByHeight rolls back the chain to the previously last
	// accepted block. If state sync has completed successfully, this call is a
	// no-op.
	if err := vm.repairAcceptedChainByHeight(); err != nil {
		return err
	}
	return vm.setLastAcceptedMetadata()
}

func (vm *VM) BuildBlock() (snowman.Block, error) {
	preferredBlock, err := vm.getBlock(vm.preferred)
	if err != nil {
		return nil, err
	}

	return preferredBlock.buildChild()
}

func (vm *VM) ParseBlock(b []byte) (snowman.Block, error) {
	if blk, err := vm.parsePostForkBlock(b); err == nil {
		return blk, nil
	}
	return vm.parsePreForkBlock(b)
}

func (vm *VM) GetBlock(id ids.ID) (snowman.Block, error) {
	return vm.getBlock(id)
}

func (vm *VM) SetPreference(preferred ids.ID) error {
	if vm.preferred == preferred {
		return nil
	}
	vm.preferred = preferred

	blk, err := vm.getPostForkBlock(preferred)
	if err != nil {
		return vm.ChainVM.SetPreference(preferred)
	}

	if err := vm.ChainVM.SetPreference(blk.getInnerBlk().ID()); err != nil {
		return err
	}

	pChainHeight, err := blk.pChainHeight()
	if err != nil {
		return err
	}

	// reset scheduler
	minDelay, err := vm.Windower.Delay(blk.Height()+1, pChainHeight, vm.ctx.NodeID)
	if err != nil {
		vm.ctx.Log.Debug("failed to fetch the expected delay",
			zap.Error(err),
		)
		// A nil error is returned here because it is possible that
		// bootstrapping caused the last accepted block to move past the latest
		// P-chain height. This will cause building blocks to return an error
		// until the P-chain's height has advanced.
		return nil
	}
	if minDelay < minBlockDelay {
		minDelay = minBlockDelay
	}

	preferredTime := blk.Timestamp()
	nextStartTime := preferredTime.Add(minDelay)
	vm.Scheduler.SetBuildBlockTime(nextStartTime)

	vm.ctx.Log.Debug("set preference",
		zap.Stringer("blkID", blk.ID()),
		zap.Time("blockTimestamp", preferredTime),
		zap.Time("nextStartTime", nextStartTime),
	)
	return nil
}

func (vm *VM) LastAccepted() (ids.ID, error) {
	lastAccepted, err := vm.State.GetLastAccepted()
	if err == database.ErrNotFound {
		return vm.ChainVM.LastAccepted()
	}
	return lastAccepted, err
}

func (vm *VM) repair(indexerState state.State) error {
	// check and possibly rebuild height index
	if vm.hVM == nil {
		return vm.repairAcceptedChainByIteration()
	}

	indexIsEmpty, err := vm.State.IsIndexEmpty()
	if err != nil {
		return err
	}
	if indexIsEmpty {
		if err := vm.State.SetIndexHasReset(); err != nil {
			return err
		}
		if err := vm.State.Commit(); err != nil {
			return err
		}
	} else {
		indexWasReset, err := vm.State.HasIndexReset()
		if err != nil {
			return fmt.Errorf("retrieving value of required index reset failed with: %w", err)
		}

		if !indexWasReset {
			vm.resetHeightIndexOngoing.SetValue(true)
		}
	}

	if !vm.resetHeightIndexOngoing.GetValue() {
		// We are not going to wipe the height index
		switch vm.hVM.VerifyHeightIndex() {
		case nil:
			// We are not going to wait for the height index to be repaired.
			shouldRepair, err := vm.shouldHeightIndexBeRepaired()
			if err != nil {
				return err
			}
			if !shouldRepair {
				vm.ctx.Log.Info("block height index was successfully verified")
				vm.hIndexer.MarkRepaired(true)
				return vm.repairAcceptedChainByHeight()
			}
		case block.ErrIndexIncomplete:
		default:
			return err
		}
	}

	if err := vm.repairAcceptedChainByIteration(); err != nil {
		return err
	}

	// asynchronously rebuild height index, if needed
	go func() {
		// If index reset has been requested, carry it out first
		if vm.resetHeightIndexOngoing.GetValue() {
			vm.ctx.Log.Info("block height indexing reset started")

			if err := indexerState.ResetHeightIndex(vm.ctx.Log, vm); err != nil {
				vm.ctx.Log.Error("block height indexing reset failed",
					zap.Error(err),
				)
				return
			}

			vm.ctx.Log.Info("block height indexing reset finished")
			vm.resetHeightIndexOngoing.SetValue(false)
		}

		// Poll until the underlying chain's index is complete or shutdown is
		// called.
		ticker := time.NewTicker(checkIndexedFrequency)
		defer ticker.Stop()
		for {
			// The underlying VM expects the lock to be held here.
			vm.ctx.Lock.Lock()
			err := vm.hVM.VerifyHeightIndex()
			vm.ctx.Lock.Unlock()

			if err == nil {
				// innerVM indexing complete. Let re-index this machine
				break
			}
			if err != block.ErrIndexIncomplete {
				vm.ctx.Log.Error("block height indexing failed",
					zap.Error(err),
				)
				return
			}

			// innerVM index is incomplete. Wait for completion and retry
			select {
			case <-vm.context.Done():
				return
			case <-ticker.C:
			}
		}

		vm.ctx.Lock.Lock()
		shouldRepair, err := vm.shouldHeightIndexBeRepaired()
		vm.ctx.Lock.Unlock()

		if err != nil {
			vm.ctx.Log.Error("could not verify height indexing status",
				zap.Error(err),
			)
			return
		}
		if !shouldRepair {
			vm.ctx.Log.Info("block height indexing is already complete")
			vm.hIndexer.MarkRepaired(true)
			return
		}

		err = vm.hIndexer.RepairHeightIndex(vm.context)
		if err == nil {
			vm.ctx.Log.Info("block height indexing finished")
			return
		}

		// Note that we don't check if `err` is `context.Canceled` here because
		// repairing the height index may have returned a non-standard error
		// due to the chain shutting down.
		if vm.context.Err() == nil {
			// The context wasn't closed, so the chain hasn't been shutdown.
			// This must have been an unexpected error.
			vm.ctx.Log.Error("block height indexing failed",
				zap.Error(err),
			)
		}
	}()
	return nil
}

func (vm *VM) repairAcceptedChainByIteration() error {
	lastAcceptedID, err := vm.GetLastAccepted()
	if err == database.ErrNotFound {
		// If the last accepted block isn't indexed yet, then the underlying
		// chain is the only chain and there is nothing to repair.
		return nil
	}
	if err != nil {
		return err
	}

	// Revert accepted blocks that weren't committed to the database.
	for {
		lastAccepted, err := vm.getPostForkBlock(lastAcceptedID)
		if err == database.ErrNotFound {
			// If the post fork block can't be found, it's because we're
			// reverting past the fork boundary. If this is the case, then there
			// is only one database to keep consistent, so there is nothing to
			// repair anymore.
			if err := vm.State.DeleteLastAccepted(); err != nil {
				return err
			}
			if err := vm.State.DeleteCheckpoint(); err != nil {
				return err
			}
			return vm.db.Commit()
		}
		if err != nil {
			return err
		}

		shouldBeAccepted := lastAccepted.getInnerBlk()

		// If the inner block is accepted, then we don't need to revert any more
		// blocks.
		if shouldBeAccepted.Status() == choices.Accepted {
			return vm.db.Commit()
		}

		// Mark the last accepted block as processing - rather than accepted.
		lastAccepted.setStatus(choices.Processing)
		if err := vm.State.PutBlock(lastAccepted.getStatelessBlk(), choices.Processing); err != nil {
			return err
		}

		// Advance to the parent block
		previousLastAcceptedID := lastAcceptedID
		lastAcceptedID = lastAccepted.Parent()
		if err := vm.State.SetLastAccepted(lastAcceptedID); err != nil {
			return err
		}

		// If the indexer checkpoint was previously pointing to the last
		// accepted block, roll it back to the new last accepted block.
		checkpoint, err := vm.State.GetCheckpoint()
		if err == database.ErrNotFound {
			continue
		}
		if err != nil {
			return err
		}
		if previousLastAcceptedID != checkpoint {
			continue
		}
		if err := vm.State.SetCheckpoint(lastAcceptedID); err != nil {
			return err
		}
	}
}

func (vm *VM) repairAcceptedChainByHeight() error {
	innerLastAcceptedID, err := vm.ChainVM.LastAccepted()
	if err != nil {
		return err
	}
	innerLastAccepted, err := vm.ChainVM.GetBlock(innerLastAcceptedID)
	if err != nil {
		return err
	}
	proLastAcceptedID, err := vm.State.GetLastAccepted()
	if err == database.ErrNotFound {
		// If the last accepted block isn't indexed yet, then the underlying
		// chain is the only chain and there is nothing to repair.
		return nil
	}
	if err != nil {
		return err
	}

	proLastAccepted, err := vm.getPostForkBlock(proLastAcceptedID)
	if err != nil {
		return err
	}

	proLastAcceptedHeight := proLastAccepted.Height()
	innerLastAcceptedHeight := innerLastAccepted.Height()
	if proLastAcceptedHeight < innerLastAcceptedHeight {
		return fmt.Errorf("proposervm height index (%d) should never be lower than the inner height index (%d)", proLastAcceptedHeight, innerLastAcceptedHeight)
	}
	if proLastAcceptedHeight == innerLastAcceptedHeight {
		// There is nothing to repair - as the heights match
		return nil
	}

	// The inner vm must be behind the proposer vm, so we must roll the proposervm back.
	forkHeight, err := vm.State.GetForkHeight()
	if err != nil {
		return err
	}

	if forkHeight > innerLastAcceptedHeight {
		// We are rolling back past the fork, so we should just forget about all of our proposervm indices.

		if err := vm.State.DeleteLastAccepted(); err != nil {
			return err
		}
		return vm.db.Commit()
	}

	newProLastAcceptedID, err := vm.State.GetBlockIDAtHeight(innerLastAcceptedHeight)
	if err != nil {
		return err
	}

	if err := vm.State.SetLastAccepted(newProLastAcceptedID); err != nil {
		return err
	}
	return vm.db.Commit()
}

func (vm *VM) setLastAcceptedMetadata() error {
	lastAcceptedID, err := vm.GetLastAccepted()
	if err == database.ErrNotFound {
		// If the last accepted block wasn't a PostFork block, then we don't
		// initialize the metadata.
		vm.lastAcceptedHeight = 0
		vm.lastAcceptedTime = time.Time{}
		return nil
	}
	if err != nil {
		return err
	}

	lastAccepted, err := vm.getPostForkBlock(lastAcceptedID)
	if err != nil {
		return err
	}

	// Set the last accepted height
	vm.lastAcceptedHeight = lastAccepted.Height()

	if _, ok := lastAccepted.getStatelessBlk().(statelessblock.SignedBlock); ok {
		// If the last accepted block wasn't a PostForkOption, then we don't
		// initialize the time.
		return nil
	}

	acceptedParent, err := vm.getPostForkBlock(lastAccepted.Parent())
	if err != nil {
		return err
	}
	vm.lastAcceptedTime = acceptedParent.Timestamp()
	return nil
}

func (vm *VM) parsePostForkBlock(b []byte) (PostForkBlock, error) {
	statelessBlock, err := statelessblock.Parse(b)
	if err != nil {
		return nil, err
	}

	// if the block already exists, then make sure the status is set correctly
	blkID := statelessBlock.ID()
	blk, err := vm.getPostForkBlock(blkID)
	if err == nil {
		return blk, nil
	}
	if err != database.ErrNotFound {
		return nil, err
	}

	innerBlkBytes := statelessBlock.Block()
	innerBlk, err := vm.parseInnerBlock(blkID, innerBlkBytes)
	if err != nil {
		return nil, err
	}

	if statelessSignedBlock, ok := statelessBlock.(statelessblock.SignedBlock); ok {
		blk = &postForkBlock{
			SignedBlock: statelessSignedBlock,
			postForkCommonComponents: postForkCommonComponents{
				vm:       vm,
				innerBlk: innerBlk,
				status:   choices.Processing,
			},
		}
	} else {
		blk = &postForkOption{
			Block: statelessBlock,
			postForkCommonComponents: postForkCommonComponents{
				vm:       vm,
				innerBlk: innerBlk,
				status:   choices.Processing,
			},
		}
	}
	return blk, nil
}

func (vm *VM) parsePreForkBlock(b []byte) (*preForkBlock, error) {
	blk, err := vm.ChainVM.ParseBlock(b)
	return &preForkBlock{
		Block: blk,
		vm:    vm,
	}, err
}

func (vm *VM) getBlock(id ids.ID) (Block, error) {
	if blk, err := vm.getPostForkBlock(id); err == nil {
		return blk, nil
	}
	return vm.getPreForkBlock(id)
}

func (vm *VM) getPostForkBlock(blkID ids.ID) (PostForkBlock, error) {
	block, exists := vm.verifiedBlocks[blkID]
	if exists {
		return block, nil
	}

	statelessBlock, status, err := vm.State.GetBlock(blkID)
	if err != nil {
		return nil, err
	}

	innerBlkBytes := statelessBlock.Block()
	innerBlk, err := vm.parseInnerBlock(blkID, innerBlkBytes)
	if err != nil {
		return nil, err
	}

	if statelessSignedBlock, ok := statelessBlock.(statelessblock.SignedBlock); ok {
		return &postForkBlock{
			SignedBlock: statelessSignedBlock,
			postForkCommonComponents: postForkCommonComponents{
				vm:       vm,
				innerBlk: innerBlk,
				status:   status,
			},
		}, nil
	}
	return &postForkOption{
		Block: statelessBlock,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vm,
			innerBlk: innerBlk,
			status:   status,
		},
	}, nil
}

func (vm *VM) getPreForkBlock(blkID ids.ID) (*preForkBlock, error) {
	blk, err := vm.ChainVM.GetBlock(blkID)
	return &preForkBlock{
		Block: blk,
		vm:    vm,
	}, err
}

func (vm *VM) storePostForkBlock(blk PostForkBlock) error {
	if err := vm.State.PutBlock(blk.getStatelessBlk(), blk.Status()); err != nil {
		return err
	}
	height := blk.Height()
	blkID := blk.ID()
	if err := vm.updateHeightIndex(height, blkID); err != nil {
		return err
	}
	return vm.db.Commit()
}

func (vm *VM) verifyAndRecordInnerBlk(postFork PostForkBlock) error {
	postForkID := postFork.ID()
	// If inner block's Verify returned true, don't call it again.
	//
	// Note that if [innerBlk.Verify] returns nil, this method returns nil. This
	// must always remain the case to maintain the inner block's invariant that
	// if it's Verify() returns nil, it is eventually accepted or rejected.
	currentInnerBlk := postFork.getInnerBlk()
	if originalInnerBlk, contains := vm.Tree.Get(currentInnerBlk); !contains {
		if err := currentInnerBlk.Verify(); err != nil {
			return err
		}
		vm.Tree.Add(currentInnerBlk)
		vm.innerBlkCache.Put(postForkID, currentInnerBlk)
	} else {
		postFork.setInnerBlk(originalInnerBlk)
	}
	vm.verifiedBlocks[postForkID] = postFork
	return nil
}

// notifyInnerBlockReady tells the scheduler that the inner VM is ready to build
// a new block
func (vm *VM) notifyInnerBlockReady() {
	select {
	case vm.toScheduler <- common.PendingTxs:
	default:
		vm.ctx.Log.Debug("dropping message to consensus engine")
	}
}

func (vm *VM) optimalPChainHeight(minPChainHeight uint64) (uint64, error) {
	minimumHeight, err := vm.ctx.ValidatorState.GetMinimumHeight()
	if err != nil {
		return 0, err
	}

	return math.Max(minimumHeight, minPChainHeight), nil
}

// parseInnerBlock attempts to parse the provided bytes as an inner block. If
// the inner block happens to be cached, then the inner block will not be
// parsed.
func (vm *VM) parseInnerBlock(outerBlkID ids.ID, innerBlkBytes []byte) (snowman.Block, error) {
	if innerBlkIntf, ok := vm.innerBlkCache.Get(outerBlkID); ok {
		return innerBlkIntf.(snowman.Block), nil
	}

	innerBlk, err := vm.ChainVM.ParseBlock(innerBlkBytes)
	if err != nil {
		return nil, err
	}
	vm.cacheInnerBlock(outerBlkID, innerBlk)
	return innerBlk, nil
}

// Caches proposervm block ID --> inner block if the inner block's height
// is within [innerBlkCacheSize] of the last accepted block's height.
func (vm *VM) cacheInnerBlock(outerBlkID ids.ID, innerBlk snowman.Block) {
	diff := math.AbsDiff(innerBlk.Height(), vm.lastAcceptedHeight)
	if diff < innerBlkCacheSize {
		vm.innerBlkCache.Put(outerBlkID, innerBlk)
	}
}
