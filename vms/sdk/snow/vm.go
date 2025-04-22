// Copyright (C) 2024, Ava Labs, Inv. All rights reserved.
// See the file LICENSE for licensing terms.

package snow

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/profiler"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/vms/sdk/event"

	"github.com/ava-labs/avalanchego/cache"
	hcontext "github.com/ava-labs/avalanchego/vms/sdk/context"
)

var _ block.StateSyncableVM = (*VM[Block, Block, Block])(nil)

type ChainInput struct {
	SnowCtx                    *snow.Context
	GenesisBytes, UpgradeBytes []byte
	ToEngine                   chan<- common.Message
	Shutdown                   <-chan struct{}
	Tracer                     trace.Tracer
	Config                     hcontext.Config
}

// Chain provides a reduced interface for chain / VM developers to implement.
// The snow package implements the full AvalancheGo VM interface, so that the chain implementation
// only needs to provide a way to initialize itself with all of the provided inputs, define concrete types
// for each state that a block could be in from the perspective of the consensus engine, and state
// transition functions between each of those types.
type Chain[I Block, O Block, A Block] interface {
	// Initialize initializes the chain, optionally configures the VM via app, and returns
	// a persistent index of the chain's input block type, the last output and accetped block,
	// and whether or not the VM is currently in a valid state. If stateReady is false, the VM
	// must be mid-state sync, such that it does not have a valid last output or accepted block.
	Initialize(
		ctx context.Context,
		chainInput ChainInput,
		vm *VM[I, O, A],
	) (inputChainIndex ChainIndex[I], lastOutput O, lastAccepted A, stateReady bool, err error)
	// SetConsensusIndex sets the ChainIndex[I, O, A} on the VM to provide the
	// VM with:
	// 1. A cached index of the chain
	// 2. The ability to fetch the latest consensus state (preferred output block and last accepted block)
	SetConsensusIndex(consensusIndex *ConsensusIndex[I, O, A])
	// BuildBlock returns a new input and output block built on top of the provided parent.
	// The provided parent will be the current preference of the consensus engine.
	BuildBlock(ctx context.Context, blockContext *block.Context, parent O) (I, O, error)
	// ParseBlock parses the provided bytes into an input block.
	ParseBlock(ctx context.Context, bytes []byte) (I, error)
	// VerifyBlock verifies the provided block is valid given its already verified parent
	// and returns the resulting output of executing the block.
	VerifyBlock(
		ctx context.Context,
		parent O,
		block I,
	) (O, error)
	// AcceptBlock marks block as accepted and returns the resulting Accepted block type.
	// AcceptBlock is guaranteed to be called after the input block has been persisted
	// to disk.
	AcceptBlock(ctx context.Context, acceptedParent A, block O) (A, error)
}

type namedCloser struct {
	name  string
	close func() error
}

type VM[I Block, O Block, A Block] struct {
	handlers        map[string]http.Handler
	network         *p2p.Network
	stateSyncableVM block.StateSyncableVM
	closers         []namedCloser

	// onNormalOperationsStarted contains callbacks that execute when the VM transitions
	// to normal operation, either after bootstrapping or state sync completion.
	onNormalOperationsStarted []func(context.Context) error

	// onBootstrapStarted contains callbacks that execute when the VM begins
	// bootstrapping state from the network.
	onBootstrapStarted []func(context.Context) error

	// onStateSyncStarted contains callbacks that execute when the VM begins
	// state sync to synchronize with the network's state.
	onStateSyncStarted []func(context.Context) error

	verifiedSubs         []event.Subscription[O]
	rejectedSubs         []event.Subscription[O]
	acceptedSubs         []event.Subscription[A]
	preReadyAcceptedSubs []event.Subscription[I]
	// preRejectedSubs handles rejections of I (Input) during/after state sync, before they reach O (Output) state
	preRejectedSubs []event.Subscription[I]
	version         string

	// chainLock provides a synchronization point between state sync and normal operation.
	// To complete dynamic state sync, we must:
	// 1. Accept a sequence of blocks from the final state sync target to the last accepted block
	// 2. Re-process all outstanding processing blocks
	// 3. Mark the VM as ready for normal operation
	//
	// During this time, we must not allow any new blocks to be verified/accepted.
	chainLock sync.Mutex
	chain     Chain[I, O, A]
	ready     bool

	inputChainIndex ChainIndex[I]
	consensusIndex  *ConsensusIndex[I, O, A]

	snowCtx  *snow.Context
	hconfig  hcontext.Config
	vmConfig VMConfig

	// Each element is a block that passed verification but
	// hasn't yet been accepted/rejected
	verifiedL      sync.RWMutex
	verifiedBlocks map[ids.ID]*StatefulBlock[I, O, A]

	// We store the last [AcceptedBlockWindowCache] blocks in memory
	// to avoid reading blocks from disk.
	acceptedBlocksByID     *cache.FIFO[ids.ID, *StatefulBlock[I, O, A]]
	acceptedBlocksByHeight *cache.FIFO[uint64, ids.ID]

	metaLock          sync.Mutex
	lastAcceptedBlock *StatefulBlock[I, O, A]
	preferredBlkID    ids.ID

	metrics *Metrics
	log     logging.Logger
	tracer  trace.Tracer

	shutdownChan chan struct{}

	healthCheckers sync.Map
}

func NewVM[I Block, O Block, A Block](version string, chain Chain[I, O, A]) *VM[I, O, A] {
	return &VM[I, O, A]{
		handlers: make(map[string]http.Handler),
		version:  version,
		chain:    chain,
	}
}

func (v *VM[I, O, A]) Initialize(
	ctx context.Context,
	chainCtx *snow.Context,
	_ database.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngine chan<- common.Message,
	_ []*common.Fx,
	appSender common.AppSender,
) error {
	v.snowCtx = chainCtx
	v.shutdownChan = make(chan struct{})

	hconfig, err := hcontext.NewConfig(configBytes)
	if err != nil {
		return fmt.Errorf("failed to create hypersdk config: %w", err)
	}
	v.hconfig = hconfig
	tracerConfig, err := GetTracerConfig(hconfig)
	if err != nil {
		return fmt.Errorf("failed to fetch tracer config: %w", err)
	}
	tracer, err := trace.New(tracerConfig)
	if err != nil {
		return err
	}
	v.tracer = tracer
	ctx, span := v.tracer.Start(ctx, "VM.Initialize")
	defer span.End()

	v.vmConfig, err = GetVMConfig(hconfig)
	if err != nil {
		return fmt.Errorf("failed to parse vm config: %w", err)
	}

	defaultRegistry, err := metrics.MakeAndRegister(v.snowCtx.Metrics, "snow")
	if err != nil {
		return err
	}
	metrics, err := newMetrics(defaultRegistry)
	if err != nil {
		return err
	}
	v.metrics = metrics
	v.log = chainCtx.Log

	continuousProfilerConfig, err := GetProfilerConfig(hconfig)
	if err != nil {
		return fmt.Errorf("failed to parse continuous profiler config: %w", err)
	}
	if continuousProfilerConfig.Enabled {
		continuousProfiler := profiler.NewContinuous(
			continuousProfilerConfig.Dir,
			continuousProfilerConfig.Freq,
			continuousProfilerConfig.MaxNumFiles,
		)
		v.addCloser("continuous profiler", func() error {
			continuousProfiler.Shutdown()
			return nil
		})
		go continuousProfiler.Dispatch() //nolint:errcheck
	}

	v.network, err = p2p.NewNetwork(v.log, appSender, defaultRegistry, "p2p")
	if err != nil {
		return fmt.Errorf("failed to initialize p2p: %w", err)
	}

	acceptedBlocksByIDCache, err := cache.NewFIFO[ids.ID, *StatefulBlock[I, O, A]](v.vmConfig.AcceptedBlockWindowCache)
	if err != nil {
		return err
	}
	v.acceptedBlocksByID = acceptedBlocksByIDCache
	acceptedBlocksByHeightCache, err := cache.NewFIFO[uint64, ids.ID](v.vmConfig.AcceptedBlockWindowCache)
	if err != nil {
		return err
	}
	v.acceptedBlocksByHeight = acceptedBlocksByHeightCache
	v.verifiedBlocks = make(map[ids.ID]*StatefulBlock[I, O, A])

	chainInput := ChainInput{
		SnowCtx:      chainCtx,
		GenesisBytes: genesisBytes,
		UpgradeBytes: upgradeBytes,
		ToEngine:     toEngine,
		Shutdown:     v.shutdownChan,
		Tracer:       v.tracer,
		Config:       v.hconfig,
	}

	inputChainIndex, lastOutput, lastAccepted, stateReady, err := v.chain.Initialize(
		ctx,
		chainInput,
		v,
	)
	if err != nil {
		return err
	}
	v.inputChainIndex = inputChainIndex
	if err := v.makeConsensusIndex(ctx, v.inputChainIndex, lastOutput, lastAccepted, stateReady); err != nil {
		return err
	}
	v.chain.SetConsensusIndex(v.consensusIndex)
	if err := v.lastAcceptedBlock.notifyAccepted(ctx); err != nil {
		return fmt.Errorf("failed to notify last accepted on startup: %w", err)
	}

	if err := v.initHealthCheckers(); err != nil {
		return err
	}

	return nil
}

func (v *VM[I, O, A]) setLastAccepted(lastAcceptedBlock *StatefulBlock[I, O, A]) {
	v.metaLock.Lock()
	defer v.metaLock.Unlock()

	v.lastAcceptedBlock = lastAcceptedBlock
	v.acceptedBlocksByHeight.Put(v.lastAcceptedBlock.Height(), v.lastAcceptedBlock.ID())
	v.acceptedBlocksByID.Put(v.lastAcceptedBlock.ID(), v.lastAcceptedBlock)
}

func (v *VM[I, O, A]) getBlockFromCache(blkID ids.ID) (*StatefulBlock[I, O, A], bool) {
	if blk, ok := v.acceptedBlocksByID.Get(blkID); ok {
		return blk, true
	}

	v.verifiedL.RLock()
	defer v.verifiedL.RUnlock()
	if blk, exists := v.verifiedBlocks[blkID]; exists {
		return blk, true
	}
	return nil, false
}

func (v *VM[I, O, A]) GetBlock(ctx context.Context, blkID ids.ID) (*StatefulBlock[I, O, A], error) {
	ctx, span := v.tracer.Start(ctx, "VM.GetBlock")
	defer span.End()

	if blk, ok := v.getBlockFromCache(blkID); ok {
		return blk, nil
	}

	// Retrieve and parse from disk
	// Note: this returns an accepted block with only the input block set.
	// The consensus engine guarantees that:
	// 1. Verify is only called on a block whose parent is lastAcceptedBlock or in verifiedBlocks
	// 2. Accept is only called on a block whose parent is lastAcceptedBlock
	blk, err := v.inputChainIndex.GetBlock(ctx, blkID)
	if err != nil {
		return nil, err
	}
	return NewInputBlock(v, blk), nil
}

func (v *VM[I, O, A]) GetBlockByHeight(ctx context.Context, height uint64) (*StatefulBlock[I, O, A], error) {
	ctx, span := v.tracer.Start(ctx, "VM.GetBlockByHeight")
	defer span.End()

	if v.lastAcceptedBlock.Height() == height {
		return v.lastAcceptedBlock, nil
	}
	var blkID ids.ID
	if fetchedBlkID, ok := v.acceptedBlocksByHeight.Get(height); ok {
		blkID = fetchedBlkID
	} else {
		fetchedBlkID, err := v.inputChainIndex.GetBlockIDAtHeight(ctx, height)
		if err != nil {
			return nil, err
		}
		blkID = fetchedBlkID
	}

	if blk, ok := v.acceptedBlocksByID.Get(blkID); ok {
		return blk, nil
	}

	return v.GetBlock(ctx, blkID)
}

func (v *VM[I, O, A]) ParseBlock(ctx context.Context, bytes []byte) (*StatefulBlock[I, O, A], error) {
	ctx, span := v.tracer.Start(ctx, "VM.ParseBlock")
	defer span.End()

	start := time.Now()
	defer func() {
		v.metrics.blockParse.Observe(float64(time.Since(start)))
	}()

	inputBlk, err := v.chain.ParseBlock(ctx, bytes)
	if err != nil {
		return nil, err
	}

	// If the block is pinned/cached, return the uniquified block
	if blk, ok := v.getBlockFromCache(inputBlk.GetID()); ok {
		return blk, nil
	}

	return NewInputBlock(v, inputBlk), nil
}

func (v *VM[I, O, A]) BuildBlockWithContext(ctx context.Context, blockCtx *block.Context) (*StatefulBlock[I, O, A], error) {
	return v.buildBlock(ctx, blockCtx)
}

func (v *VM[I, O, A]) BuildBlock(ctx context.Context) (*StatefulBlock[I, O, A], error) {
	return v.buildBlock(ctx, nil)
}

func (v *VM[I, O, A]) buildBlock(ctx context.Context, blockCtx *block.Context) (*StatefulBlock[I, O, A], error) {
	v.chainLock.Lock()
	defer v.chainLock.Unlock()

	ctx, span := v.tracer.Start(ctx, "VM.BuildBlock")
	defer span.End()

	start := time.Now()
	defer func() {
		v.metrics.blockBuild.Observe(float64(time.Since(start)))
	}()

	preferredBlk, err := v.GetBlock(ctx, v.preferredBlkID)
	if err != nil {
		return nil, fmt.Errorf("failed to get preferred block %s to build: %w", v.preferredBlkID, err)
	}
	inputBlock, outputBlock, err := v.chain.BuildBlock(ctx, blockCtx, preferredBlk.Output)
	if err != nil {
		return nil, err
	}
	sb := NewVerifiedBlock[I, O, A](v, inputBlock, outputBlock)

	return sb, nil
}

func (v *VM[I, O, A]) LastAcceptedBlock(_ context.Context) *StatefulBlock[I, O, A] {
	return v.lastAcceptedBlock
}

func (v *VM[I, O, A]) GetBlockIDAtHeight(ctx context.Context, blkHeight uint64) (ids.ID, error) {
	ctx, span := v.tracer.Start(ctx, "VM.GetBlockIDAtHeight")
	defer span.End()

	if blkHeight == v.lastAcceptedBlock.Height() {
		return v.lastAcceptedBlock.ID(), nil
	}
	if blkID, ok := v.acceptedBlocksByHeight.Get(blkHeight); ok {
		return blkID, nil
	}
	return v.inputChainIndex.GetBlockIDAtHeight(ctx, blkHeight)
}

func (v *VM[I, O, A]) SetPreference(_ context.Context, blkID ids.ID) error {
	v.metaLock.Lock()
	defer v.metaLock.Unlock()

	v.preferredBlkID = blkID
	return nil
}

func (v *VM[I, O, A]) LastAccepted(context.Context) (ids.ID, error) {
	return v.lastAcceptedBlock.ID(), nil
}

func (v *VM[I, O, A]) SetState(ctx context.Context, state snow.State) error {
	v.log.Info("Setting state to %s", zap.Stringer("state", state))
	switch state {
	case snow.StateSyncing:
		for _, startStateSyncF := range v.onStateSyncStarted {
			if err := startStateSyncF(ctx); err != nil {
				return err
			}
		}
		return nil
	case snow.Bootstrapping:
		for _, startBootstrappingF := range v.onBootstrapStarted {
			if err := startBootstrappingF(ctx); err != nil {
				return err
			}
		}
		return nil
	case snow.NormalOp:
		for _, startNormalOpF := range v.onNormalOperationsStarted {
			if err := startNormalOpF(ctx); err != nil {
				return err
			}
		}
		return nil
	default:
		return snow.ErrUnknownState
	}
}

func (v *VM[I, O, A]) CreateHandlers(_ context.Context) (map[string]http.Handler, error) {
	return v.handlers, nil
}

func (v *VM[I, O, A]) Shutdown(context.Context) error {
	v.log.Info("Shutting down VM")
	close(v.shutdownChan)

	errs := make([]error, len(v.closers))
	for i, closer := range v.closers {
		v.log.Info("Shutting down service", zap.String("service", closer.name))
		start := time.Now()
		errs[i] = closer.close()
		v.log.Info("Finished shutting down service", zap.String("service", closer.name), zap.Duration("duration", time.Since(start)))
	}
	return errors.Join(errs...)
}

func (v *VM[I, O, A]) Version(context.Context) (string, error) {
	return v.version, nil
}

func (v *VM[I, O, A]) addCloser(name string, closer func() error) {
	v.closers = append(v.closers, namedCloser{name, closer})
}

func (v *VM[I, O, A]) GetInputCovariantVM() *InputCovariantVM[I, O, A] {
	return &InputCovariantVM[I, O, A]{vm: v}
}

func (v *VM[I, O, A]) GetNetwork() *p2p.Network {
	return v.network
}

func (v *VM[I, O, A]) AddAcceptedSub(sub ...event.Subscription[A]) {
	v.acceptedSubs = append(v.acceptedSubs, sub...)
}

func (v *VM[I, O, A]) AddRejectedSub(sub ...event.Subscription[O]) {
	v.rejectedSubs = append(v.rejectedSubs, sub...)
}

func (v *VM[I, O, A]) AddVerifiedSub(sub ...event.Subscription[O]) {
	v.verifiedSubs = append(v.verifiedSubs, sub...)
}

func (v *VM[I, O, A]) AddPreReadyAcceptedSub(sub ...event.Subscription[I]) {
	v.preReadyAcceptedSubs = append(v.preReadyAcceptedSubs, sub...)
}

// AddPreRejectedSub adds subscriptions tracking rejected blocks that were
// vacuously verified during state sync before the VM had the state to verify them
func (v *VM[I, O, A]) AddPreRejectedSub(sub ...event.Subscription[I]) {
	v.preRejectedSubs = append(v.preRejectedSubs, sub...)
}

func (v *VM[I, O, A]) AddHandler(name string, handler http.Handler) {
	v.handlers[name] = handler
}

func (v *VM[I, O, A]) AddCloser(name string, closer func() error) {
	v.addCloser(name, closer)
}

// AddStateSyncStarter registers a callback that will be executed when the engine invokes SetState(snow.StateSyncing)
// i.e., when it's in state sync operation
func (v *VM[I, O, A]) AddStateSyncStarter(onStateSyncStarted ...func(context.Context) error) {
	v.onStateSyncStarted = append(v.onStateSyncStarted, onStateSyncStarted...)
}

// AddNormalOpStarter registers a callback that will be executed when the engine invokes SetState(snow.NormalOp)
// i.e., transitioning from state sync / bootstrapping to normal operation.
func (v *VM[I, O, A]) AddNormalOpStarter(onNormalOpStartedF ...func(context.Context) error) {
	v.onNormalOperationsStarted = append(v.onNormalOperationsStarted, onNormalOpStartedF...)
}
