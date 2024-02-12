// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/proposervm/indexer"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
	"github.com/ava-labs/avalanchego/vms/proposervm/scheduler"
	"github.com/ava-labs/avalanchego/vms/proposervm/state"
	"github.com/ava-labs/avalanchego/vms/proposervm/tree"

	statelessblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

const (
	// DefaultMinBlockDelay should be kept as whole seconds because block
	// timestamps are only specific to the second.
	DefaultMinBlockDelay = time.Second
	// DefaultNumHistoricalBlocks as 0 results in never deleting any historical
	// blocks.
	DefaultNumHistoricalBlocks uint64 = 0

	checkIndexedFrequency = 10 * time.Second
	innerBlkCacheSize     = 64 * units.MiB
)

var (
	_ block.ChainVM         = (*VM)(nil)
	_ block.BatchedChainVM  = (*VM)(nil)
	_ block.StateSyncableVM = (*VM)(nil)

	// TODO: remove after the X-chain supports height indexing.
	mainnetXChainID = ids.FromStringOrPanic("2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM")
	fujiXChainID    = ids.FromStringOrPanic("2JVSBoinj9C2J33VntvzYtVJNZdN2NKiwwKjcumHUWEb5DbBrm")

	dbPrefix = []byte("proposervm")

	errHeightIndexInvalidWhilePruning = errors.New("height index invalid while pruning old blocks")
)

func cachedBlockSize(_ ids.ID, blk snowman.Block) int {
	return ids.IDLen + len(blk.Bytes()) + constants.PointerOverhead
}

type VM struct {
	block.ChainVM
	Config
	blockBuilderVM block.BuildBlockWithContextChainVM
	batchedVM      block.BatchedChainVM
	ssVM           block.StateSyncableVM

	state.State
	hIndexer indexer.HeightIndexer

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
	innerBlkCache  cache.Cacher[ids.ID, snowman.Block]
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

// New performs best when [minBlkDelay] is whole seconds. This is because block
// timestamps are only specific to the second.
func New(
	vm block.ChainVM,
	config Config,
) *VM {
	blockBuilderVM, _ := vm.(block.BuildBlockWithContextChainVM)
	batchedVM, _ := vm.(block.BatchedChainVM)
	ssVM, _ := vm.(block.StateSyncableVM)
	return &VM{
		ChainVM:        vm,
		Config:         config,
		blockBuilderVM: blockBuilderVM,
		batchedVM:      batchedVM,
		ssVM:           ssVM,
	}
}

func (vm *VM) Initialize(
	ctx context.Context,
	chainCtx *snow.Context,
	db database.Database,
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
	if err := chainCtx.Metrics.Register(multiGatherer); err != nil {
		return err
	}
	chainCtx.Metrics = optionalGatherer

	vm.ctx = chainCtx
	vm.db = versiondb.New(prefixdb.New(dbPrefix, db))
	baseState, err := state.NewMetered(vm.db, "state", registerer)
	if err != nil {
		return err
	}
	vm.State = baseState
	vm.Windower = proposer.New(chainCtx.ValidatorState, chainCtx.SubnetID, chainCtx.ChainID)
	vm.Tree = tree.New()
	innerBlkCache, err := metercacher.New(
		"inner_block_cache",
		registerer,
		cache.NewSizedLRU(
			innerBlkCacheSize,
			cachedBlockSize,
		),
	)
	if err != nil {
		return err
	}
	vm.innerBlkCache = innerBlkCache

	indexerDB := versiondb.New(vm.db)
	indexerState := state.New(indexerDB)
	vm.hIndexer = indexer.NewHeightIndexer(vm, vm.ctx.Log, indexerState)

	scheduler, vmToEngine := scheduler.New(vm.ctx.Log, toEngine)
	vm.Scheduler = scheduler
	vm.toScheduler = vmToEngine

	go chainCtx.Log.RecoverAndPanic(func() {
		scheduler.Dispatch(time.Now())
	})

	vm.verifiedBlocks = make(map[ids.ID]PostForkBlock)
	detachedCtx := context.WithoutCancel(ctx)
	context, cancel := context.WithCancel(detachedCtx)
	vm.context = context
	vm.onShutdown = cancel

	err = vm.ChainVM.Initialize(
		ctx,
		chainCtx,
		db,
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

	if err := vm.repair(detachedCtx); err != nil {
		return err
	}

	if err := vm.setLastAcceptedMetadata(ctx); err != nil {
		return err
	}

	if err := vm.pruneOldBlocks(); err != nil {
		return err
	}

	forkHeight, err := vm.getForkHeight()
	switch err {
	case nil:
		chainCtx.Log.Info("initialized proposervm",
			zap.String("state", "after fork"),
			zap.Uint64("forkHeight", forkHeight),
			zap.Uint64("lastAcceptedHeight", vm.lastAcceptedHeight),
		)
	case database.ErrNotFound:
		chainCtx.Log.Info("initialized proposervm",
			zap.String("state", "before fork"),
		)
	default:
		return err
	}
	return nil
}

// shutdown ops then propagate shutdown to innerVM
func (vm *VM) Shutdown(ctx context.Context) error {
	vm.onShutdown()

	vm.Scheduler.Close()

	if err := vm.db.Commit(); err != nil {
		return err
	}
	return vm.ChainVM.Shutdown(ctx)
}

func (vm *VM) SetState(ctx context.Context, newState snow.State) error {
	if err := vm.ChainVM.SetState(ctx, newState); err != nil {
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
	if err := vm.repairAcceptedChainByHeight(ctx); err != nil {
		return err
	}
	return vm.setLastAcceptedMetadata(ctx)
}

func (vm *VM) BuildBlock(ctx context.Context) (snowman.Block, error) {
	preferredBlock, err := vm.getBlock(ctx, vm.preferred)
	if err != nil {
		vm.ctx.Log.Error("unexpected build block failure",
			zap.String("reason", "failed to fetch preferred block"),
			zap.Stringer("parentID", vm.preferred),
			zap.Error(err),
		)
		return nil, err
	}

	return preferredBlock.buildChild(ctx)
}

func (vm *VM) ParseBlock(ctx context.Context, b []byte) (snowman.Block, error) {
	if blk, err := vm.parsePostForkBlock(ctx, b); err == nil {
		return blk, nil
	}
	return vm.parsePreForkBlock(ctx, b)
}

func (vm *VM) GetBlock(ctx context.Context, id ids.ID) (snowman.Block, error) {
	return vm.getBlock(ctx, id)
}

func (vm *VM) SetPreference(ctx context.Context, preferred ids.ID) error {
	if vm.preferred == preferred {
		return nil
	}
	vm.preferred = preferred

	blk, err := vm.getPostForkBlock(ctx, preferred)
	if err != nil {
		return vm.ChainVM.SetPreference(ctx, preferred)
	}

	if err := vm.ChainVM.SetPreference(ctx, blk.getInnerBlk().ID()); err != nil {
		return err
	}

	pChainHeight, err := blk.pChainHeight(ctx)
	if err != nil {
		return err
	}

	var (
		childBlockHeight = blk.Height() + 1
		parentTimestamp  = blk.Timestamp()
		nextStartTime    time.Time
	)
	if vm.IsDurangoActivated(parentTimestamp) {
		currentTime := vm.Clock.Time().Truncate(time.Second)
		nextStartTime, err = vm.getPostDurangoSlotTime(
			ctx,
			childBlockHeight,
			pChainHeight,
			proposer.TimeToSlot(parentTimestamp, currentTime),
			parentTimestamp,
		)
	} else {
		nextStartTime, err = vm.getPreDurangoSlotTime(
			ctx,
			childBlockHeight,
			pChainHeight,
			parentTimestamp,
		)
	}
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
	vm.Scheduler.SetBuildBlockTime(nextStartTime)

	vm.ctx.Log.Debug("set preference",
		zap.Stringer("blkID", blk.ID()),
		zap.Time("blockTimestamp", parentTimestamp),
		zap.Time("nextStartTime", nextStartTime),
	)
	return nil
}

func (vm *VM) getPreDurangoSlotTime(
	ctx context.Context,
	blkHeight,
	pChainHeight uint64,
	parentTimestamp time.Time,
) (time.Time, error) {
	delay, err := vm.Windower.Delay(ctx, blkHeight, pChainHeight, vm.ctx.NodeID, proposer.MaxBuildWindows)
	if err != nil {
		return time.Time{}, err
	}

	// Note: The P-chain does not currently try to target any block time. It
	// notifies the consensus engine as soon as a new block may be built. To
	// avoid fast runs of blocks there is an additional minimum delay that
	// validators can specify. This delay may be an issue for high performance,
	// custom VMs. Until the P-chain is modified to target a specific block
	// time, ProposerMinBlockDelay can be configured in the subnet config.
	delay = max(delay, vm.MinBlkDelay)
	return parentTimestamp.Add(delay), nil
}

func (vm *VM) getPostDurangoSlotTime(
	ctx context.Context,
	blkHeight,
	pChainHeight,
	slot uint64,
	parentTimestamp time.Time,
) (time.Time, error) {
	delay, err := vm.Windower.MinDelayForProposer(
		ctx,
		blkHeight,
		pChainHeight,
		vm.ctx.NodeID,
		slot,
	)
	// Note: The P-chain does not currently try to target any block time. It
	// notifies the consensus engine as soon as a new block may be built. To
	// avoid fast runs of blocks there is an additional minimum delay that
	// validators can specify. This delay may be an issue for high performance,
	// custom VMs. Until the P-chain is modified to target a specific block
	// time, ProposerMinBlockDelay can be configured in the subnet config.
	switch {
	case err == nil:
		delay = max(delay, vm.MinBlkDelay)
		return parentTimestamp.Add(delay), err
	case errors.Is(err, proposer.ErrAnyoneCanPropose):
		return parentTimestamp.Add(vm.MinBlkDelay), err
	default:
		return time.Time{}, err
	}
}

func (vm *VM) LastAccepted(ctx context.Context) (ids.ID, error) {
	lastAccepted, err := vm.State.GetLastAccepted()
	if err == database.ErrNotFound {
		return vm.ChainVM.LastAccepted(ctx)
	}
	return lastAccepted, err
}

// repair makes sure that vm and innerVM chains are in sync.
// Moreover it fixes vm's height index if defined.
func (vm *VM) repair(ctx context.Context) error {
	switch err := vm.ChainVM.VerifyHeightIndex(ctx); err {
	case nil:
		// InnerVM height index is complete. We can immediately verify
		// and repair this VM height index.
		shouldRepair, err := vm.shouldHeightIndexBeRepaired(ctx)
		if err != nil {
			return err
		}
		if !shouldRepair {
			vm.ctx.Log.Info("block height index was successfully verified")
			vm.hIndexer.MarkRepaired(true)
			return vm.repairAcceptedChainByHeight(ctx)
		}
	case block.ErrIndexIncomplete:
	default:
		return err
	}

	if vm.NumHistoricalBlocks != 0 {
		vm.ctx.Log.Fatal("block height index must be valid when pruning historical blocks")
		return errHeightIndexInvalidWhilePruning
	}

	// innerVM height index is incomplete. Sync vm and innerVM chains first.
	if err := vm.repairAcceptedChainByIteration(ctx); err != nil {
		return err
	}

	// asynchronously rebuild height index, if needed
	go func() {
		// Poll until the underlying chain's index is complete or shutdown is
		// called.
		ticker := time.NewTicker(checkIndexedFrequency)
		defer ticker.Stop()
		for {
			// The underlying VM expects the lock to be held here.
			vm.ctx.Lock.Lock()
			err := vm.ChainVM.VerifyHeightIndex(ctx)
			vm.ctx.Lock.Unlock()

			if err == nil {
				// innerVM indexing complete. Let's re-index this VM
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
		shouldRepair, err := vm.shouldHeightIndexBeRepaired(ctx)
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

func (vm *VM) repairAcceptedChainByIteration(ctx context.Context) error {
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
		lastAccepted, err := vm.getPostForkBlock(ctx, lastAcceptedID)
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

func (vm *VM) repairAcceptedChainByHeight(ctx context.Context) error {
	innerLastAcceptedID, err := vm.ChainVM.LastAccepted(ctx)
	if err != nil {
		return err
	}
	innerLastAccepted, err := vm.ChainVM.GetBlock(ctx, innerLastAcceptedID)
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
	proLastAccepted, err := vm.getPostForkBlock(ctx, proLastAcceptedID)
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

	vm.ctx.Log.Info("repairing accepted chain by height",
		zap.Uint64("outerHeight", proLastAcceptedHeight),
		zap.Uint64("innerHeight", innerLastAcceptedHeight),
	)

	// The inner vm must be behind the proposer vm, so we must roll the
	// proposervm back.
	forkHeight, err := vm.State.GetForkHeight()
	if err != nil {
		return err
	}

	if forkHeight > innerLastAcceptedHeight {
		// We are rolling back past the fork, so we should just forget about all
		// of our proposervm indices.
		if err := vm.State.DeleteLastAccepted(); err != nil {
			return err
		}
		return vm.db.Commit()
	}

	newProLastAcceptedID, err := vm.State.GetBlockIDAtHeight(innerLastAcceptedHeight)
	if err != nil {
		// This fatal error can happen if NumHistoricalBlocks is set too
		// aggressively and the inner vm rolled back before the oldest
		// proposervm block.
		return fmt.Errorf("proposervm failed to rollback last accepted block to height (%d): %w", innerLastAcceptedHeight, err)
	}

	if err := vm.State.SetLastAccepted(newProLastAcceptedID); err != nil {
		return err
	}
	return vm.db.Commit()
}

func (vm *VM) setLastAcceptedMetadata(ctx context.Context) error {
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

	lastAccepted, err := vm.getPostForkBlock(ctx, lastAcceptedID)
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

	acceptedParent, err := vm.getPostForkBlock(ctx, lastAccepted.Parent())
	if err != nil {
		return err
	}
	vm.lastAcceptedTime = acceptedParent.Timestamp()
	return nil
}

func (vm *VM) parsePostForkBlock(ctx context.Context, b []byte) (PostForkBlock, error) {
	statelessBlock, err := statelessblock.Parse(b, vm.DurangoTime)
	if err != nil {
		return nil, err
	}

	// if the block already exists, then make sure the status is set correctly
	blkID := statelessBlock.ID()
	blk, err := vm.getPostForkBlock(ctx, blkID)
	if err == nil {
		return blk, nil
	}
	if err != database.ErrNotFound {
		return nil, err
	}

	innerBlkBytes := statelessBlock.Block()
	innerBlk, err := vm.parseInnerBlock(ctx, blkID, innerBlkBytes)
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

func (vm *VM) parsePreForkBlock(ctx context.Context, b []byte) (*preForkBlock, error) {
	blk, err := vm.ChainVM.ParseBlock(ctx, b)
	return &preForkBlock{
		Block: blk,
		vm:    vm,
	}, err
}

func (vm *VM) getBlock(ctx context.Context, id ids.ID) (Block, error) {
	if blk, err := vm.getPostForkBlock(ctx, id); err == nil {
		return blk, nil
	}
	return vm.getPreForkBlock(ctx, id)
}

// TODO: remove after the P-chain and X-chain support height indexing.
func (vm *VM) getForkHeight() (uint64, error) {
	// The fork block can be easily identified with the provided links because
	// the `Parent Hash` is equal to the `Proposer Parent ID`.
	switch vm.ctx.ChainID {
	case constants.PlatformChainID:
		switch vm.ctx.NetworkID {
		case constants.MainnetID:
			return 805732, nil // https://subnets.avax.network/p-chain/block/805732
		case constants.FujiID:
			return 47529, nil // https://subnets-test.avax.network/p-chain/block/47529
		}
	case mainnetXChainID:
		return 1, nil // https://subnets.avax.network/x-chain/block/1
	case fujiXChainID:
		return 1, nil // https://subnets-test.avax.network/x-chain/block/1
	}
	return vm.GetForkHeight()
}

func (vm *VM) getPostForkBlock(ctx context.Context, blkID ids.ID) (PostForkBlock, error) {
	block, exists := vm.verifiedBlocks[blkID]
	if exists {
		return block, nil
	}

	statelessBlock, status, err := vm.State.GetBlock(blkID)
	if err != nil {
		return nil, err
	}

	innerBlkBytes := statelessBlock.Block()
	innerBlk, err := vm.parseInnerBlock(ctx, blkID, innerBlkBytes)
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

func (vm *VM) getPreForkBlock(ctx context.Context, blkID ids.ID) (*preForkBlock, error) {
	blk, err := vm.ChainVM.GetBlock(ctx, blkID)
	return &preForkBlock{
		Block: blk,
		vm:    vm,
	}, err
}

func (vm *VM) acceptPostForkBlock(blk PostForkBlock) error {
	height := blk.Height()
	blkID := blk.ID()

	vm.lastAcceptedHeight = height
	delete(vm.verifiedBlocks, blkID)

	// Persist this block, its height index, and its status
	if err := vm.State.SetLastAccepted(blkID); err != nil {
		return err
	}
	if err := vm.State.PutBlock(blk.getStatelessBlk(), choices.Accepted); err != nil {
		return err
	}
	if err := vm.updateHeightIndex(height, blkID); err != nil {
		return err
	}
	return vm.db.Commit()
}

func (vm *VM) verifyAndRecordInnerBlk(ctx context.Context, blockCtx *block.Context, postFork PostForkBlock) error {
	innerBlk := postFork.getInnerBlk()
	postForkID := postFork.ID()
	originalInnerBlock, previouslyVerified := vm.Tree.Get(innerBlk)
	if previouslyVerified {
		innerBlk = originalInnerBlock
		// We must update all of the mappings from postFork -> innerBlock to
		// now point to originalInnerBlock.
		postFork.setInnerBlk(originalInnerBlock)
		vm.innerBlkCache.Put(postForkID, originalInnerBlock)
	}

	var (
		shouldVerifyWithCtx = blockCtx != nil
		blkWithCtx          block.WithVerifyContext
		err                 error
	)
	if shouldVerifyWithCtx {
		blkWithCtx, shouldVerifyWithCtx = innerBlk.(block.WithVerifyContext)
		if shouldVerifyWithCtx {
			shouldVerifyWithCtx, err = blkWithCtx.ShouldVerifyWithContext(ctx)
			if err != nil {
				return err
			}
		}
	}

	// Invariant: If either [Verify] or [VerifyWithContext] returns nil, this
	//            function must return nil. This maintains the inner block's
	//            invariant that successful verification will eventually result
	//            in accepted or rejected being called.
	if shouldVerifyWithCtx {
		// This block needs to know the P-Chain height during verification.
		// Note that [VerifyWithContext] with context may be called multiple
		// times with multiple contexts.
		err = blkWithCtx.VerifyWithContext(ctx, blockCtx)
	} else if !previouslyVerified {
		// This isn't a [block.WithVerifyContext] so we only call [Verify] once.
		err = innerBlk.Verify(ctx)
	}
	if err != nil {
		return err
	}

	// Since verification passed, we should ensure the inner block tree is
	// populated.
	if !previouslyVerified {
		vm.Tree.Add(innerBlk)
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

func (vm *VM) optimalPChainHeight(ctx context.Context, minPChainHeight uint64) (uint64, error) {
	minimumHeight, err := vm.ctx.ValidatorState.GetMinimumHeight(ctx)
	if err != nil {
		return 0, err
	}

	return max(minimumHeight, minPChainHeight), nil
}

// parseInnerBlock attempts to parse the provided bytes as an inner block. If
// the inner block happens to be cached, then the inner block will not be
// parsed.
func (vm *VM) parseInnerBlock(ctx context.Context, outerBlkID ids.ID, innerBlkBytes []byte) (snowman.Block, error) {
	if innerBlk, ok := vm.innerBlkCache.Get(outerBlkID); ok {
		return innerBlk, nil
	}

	innerBlk, err := vm.ChainVM.ParseBlock(ctx, innerBlkBytes)
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
