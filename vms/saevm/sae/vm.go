// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/txpool"
	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/event"
	"github.com/ava-labs/libevm/params"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/sae/rpc"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
	"github.com/ava-labs/avalanchego/vms/saevm/saexec"
	"github.com/ava-labs/avalanchego/vms/saevm/txgossip"

	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

// VM implements all of [adaptor.ChainVM] except for the `Initialize` method,
// which needs to be provided by a harness. In all cases, the harness MUST
// provide a last-synchronous block, which MAY be the genesis.
type VM struct {
	*p2p.Network
	Peers          *p2p.Peers
	ValidatorPeers *p2p.Validators

	hooks       hook.Points
	config      Config
	snowCtx     *snow.Context
	metrics     *prometheus.Registry
	chainConfig *params.ChainConfig

	afterSync     func(context.Context, *blocks.Block) error
	stateSyncDone chan error

	db  ethdb.Database
	xdb saetypes.ExecutionResults

	consensusState utils.Atomic[snow.State]

	preference atomic.Pointer[blocks.Block]
	last       struct {
		accepted, settled atomic.Pointer[blocks.Block]
		synchronous       uint64
	}
	acceptedBlocks event.FeedOf[*blocks.Block]
	// Consensus-critical blocks are those either (a) undergoing a consensus
	// decision; or (b) informing consensus invariants (e.g. artefacts to
	// settle). The latter is defined as the history of accepted blocks up to,
	// and including, the last-settled block.
	consensusCritical *syncMap[common.Hash, *blocks.Block]

	exec         *saexec.Executor
	mempool      *txgossip.Set
	blockBuilder blockBuilder
	rpcProvider  *rpc.Provider
	newTxs       chan struct{}

	// toClose are closed in reverse order during [VM.Shutdown]. If a resource
	// depends on another resource, it MUST be added AFTER the resource it
	// depends on.
	toClose []io.Closer
}

// closerFunc adapts a func() error to [io.Closer].
type closerFunc func() error

var _ io.Closer = (*closerFunc)(nil)

func (f closerFunc) Close() error { return f() }

// A Config configures construction of a new [VM].
type Config struct {
	MempoolConfig legacypool.Config
	DBConfig      saedb.Config
	RPCConfig     rpc.Config

	StateSyncEnabled           bool
	ExcessAfterLastSynchronous gas.Gas

	Now func() time.Time // defaults to [time.Now] if nil
}

// NewVM returns a new [VM] that is ready for use immediately upon return.
// [VM.Shutdown] MUST be called to release resources.
//
// The state root of the last synchronous block MUST be available when creating
// a [triedb.Database] from the provided [ethdb.Database] and [triedb.Config]
// (the latter provided via the [Config]).
func NewVM[T hook.Transaction](
	ctx context.Context,
	hooks hook.PointsG[T],
	cfg Config,
	snowCtx *snow.Context,
	chainConfig *params.ChainConfig,
	db ethdb.Database,
	lastSynchronous *types.Block,
	sender snowcommon.AppSender,
) (_ *VM, retErr error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	vm := &VM{
		hooks:         hooks,
		config:        cfg,
		snowCtx:       snowCtx,
		metrics:       prometheus.NewRegistry(),
		db:            db,
		chainConfig:   chainConfig,
		stateSyncDone: make(chan error, 1),
	}
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, vm.close())
		}
	}()

	if err := snowCtx.Metrics.Register("sae", vm.metrics); err != nil {
		return nil, err
	}

	network, peers, validatorPeers, err := newNetwork(snowCtx, sender, vm.metrics)
	if err != nil {
		return nil, fmt.Errorf("newNetwork(...): %v", err)
	}
	vm.Network = network
	vm.Peers = peers
	vm.ValidatorPeers = validatorPeers

	xdb, err := hooks.ExecutionResultsDB(
		filepath.Join(snowCtx.ChainDataDir, "sae_execution_results"),
	)
	if err != nil {
		return nil, fmt.Errorf("%T.ExecutionResultsDB(%q): %v", hooks, snowCtx.ChainDataDir, err)
	}
	vm.xdb = xdb
	vm.toClose = append(vm.toClose, &xdb)

	// We guarantee another entrypoint to initialize the executor.
	// The last synchronous block likely isn't known in this case.
	if cfg.StateSyncEnabled {
		// Block map will be overwritten after sync
		noop := func(*blocks.Block) {}
		vm.consensusCritical = newSyncMap[common.Hash, *blocks.Block](noop, noop)
		vm.afterSync = onCaughtUp(vm, hooks)
		return vm, nil
	}

	lastSync, err := blocks.New(lastSynchronous, nil, nil, vm.snowCtx.Log)
	if err != nil {
		return nil, fmt.Errorf("blocks.New([last synchronous], ...): %v", err)
	}
	vm.last.synchronous = lastSync.Height()

	{ // ==========  Sync -> Async  ==========
		if err := lastSync.MarkSynchronous(hooks, vm.db, vm.xdb, vm.config.ExcessAfterLastSynchronous); err != nil {
			return nil, fmt.Errorf("%T{genesis}.MarkSynchronous(): %v", lastSync, err)
		}
		if err := canonicaliseLastSynchronous(vm.db, lastSync); err != nil {
			return nil, err
		}
	}
	if err := initializeStateful(vm, ctx, lastSync, hooks); err != nil {
		return nil, err
	}

	return vm, nil
}

func onCaughtUp[T hook.Transaction](vm *VM, hooks hook.PointsG[T]) func(ctx context.Context, firstKnown *blocks.Block) error {
	return func(ctx context.Context, firstKnown *blocks.Block) error {
		return initializeStateful(vm, ctx, firstKnown, hooks)
	}
}

// TODO: generic methods!
func initializeStateful[T hook.Transaction](vm *VM, ctx context.Context, firstKnown *blocks.Block, hooks hook.PointsG[T]) error {
	{ // ==========  Block State  ==========
		rec := &recovery{vm.db, vm.xdb, vm.chainConfig, vm.snowCtx.Log, hooks, vm.config, firstKnown}
		lastCommitted, err := rec.lastCommittedBlock()
		if err != nil {
			return err
		}

		exec, err := saexec.New(
			lastCommitted,
			vm.headerSource,
			vm.chainConfig,
			vm.db,
			vm.xdb,
			vm.config.DBConfig,
			hooks,
			vm.snowCtx.Log,
		)
		if err != nil {
			return fmt.Errorf("saexec.New(...): %v", err)
		}
		vm.exec = exec
		vm.toClose = append(vm.toClose, exec)

		if err := rec.executeAllAccepted(ctx, exec); err != nil {
			return err
		}

		bMap, lastSettled, err := rec.consensusCriticalBlocks(exec)
		if err != nil {
			return err
		}
		vm.consensusCritical = bMap

		head := exec.LastExecuted()
		vm.last.settled.Store(lastSettled)
		vm.last.accepted.Store(head)
		vm.preference.Store(head)
	}

	{ // ==========  Mempool  ==========
		bc := txgossip.NewBlockChain(vm.exec, vm.ethBlockSource)
		pools := []txpool.SubPool{
			legacypool.New(vm.config.MempoolConfig, bc),
		}
		txPool, err := txpool.New(0, bc, pools)
		if err != nil {
			return fmt.Errorf("txpool.New(...): %v", err)
		}
		vm.toClose = append(vm.toClose, txPool)

		metrics, err := bloom.NewMetrics("mempool", vm.metrics)
		if err != nil {
			return err
		}
		conf := gossip.BloomSetConfig{Metrics: metrics}
		pool, err := txgossip.NewSet(txPool, conf)
		if err != nil {
			return err
		}
		vm.mempool = pool
		vm.signalNewTxsToEngine()
	}

	{ // ==========  Block Builder  ==========
		vm.blockBuilder = &blockBuilderG[T]{
			hooks,
			vm.config.Now,
			vm.snowCtx.Log,
			vm.exec,
			vm.mempool,
			vm.ethBlockSource,
		}
	}

	{ // ==========  P2P Gossip  ==========
		const pullGossipPeriod = time.Second
		handler, pullGossiper, pushGossiper, err := gossip.NewSystem(
			vm.snowCtx.NodeID,
			vm.Network,
			vm.ValidatorPeers,
			vm.mempool,
			txgossip.Marshaller{},
			gossip.SystemConfig{
				Log:           vm.snowCtx.Log,
				Registry:      vm.metrics,
				Namespace:     "gossip",
				RequestPeriod: pullGossipPeriod,
			},
		)
		if err != nil {
			return fmt.Errorf("gossip.NewSystem(...): %v", err)
		}
		if err := vm.AddHandler(p2p.TxGossipHandlerID, handler); err != nil {
			return fmt.Errorf("network.AddHandler(...): %v", err)
		}

		var (
			gossipCtx, cancel = context.WithCancel(context.Background())
			wg                sync.WaitGroup
		)
		wg.Add(2)
		go func() {
			defer wg.Done()
			gossip.Every(gossipCtx, vm.snowCtx.Log, pullGossiper, pullGossipPeriod)
		}()
		go func() {
			defer wg.Done()
			const pushGossipPeriod = 100 * time.Millisecond
			gossip.Every(gossipCtx, vm.snowCtx.Log, pushGossiper, pushGossipPeriod)
		}()

		vm.mempool.RegisterPushGossiper(pushGossiper)
		vm.toClose = append(vm.toClose, closerFunc(func() error {
			cancel()
			wg.Wait()
			return nil
		}))
	}

	// TODO: move to the `NewVM` method once I define the path, and just replace the backend later.
	{ // ==========  RPC Provider  ==========
		r, err := rpc.New(chain{vm}, vm.config.RPCConfig)
		if err != nil {
			return err
		}
		vm.toClose = append(vm.toClose, r)
		vm.rpcProvider = r
	}

	return nil
}

// canonicaliseLastSynchronous writes all necessary information to the database
// to have the block be considered accepted/canonical by SAE. If there are any
// canonical blocks at a height greater than the provided block then this
// function is a no-op, which makes it effectively idempotent with respect to
// the rest of SAE processing.
func canonicaliseLastSynchronous(db ethdb.Database, block *blocks.Block) error {
	if !block.Synchronous() {
		return fmt.Errorf("only synchronous block can be canonicalised: %d / %#x is async", block.NumberU64(), block.Hash())
	}
	num := block.NumberU64()
	if rawdb.ReadCanonicalHash(db, num+1) != (common.Hash{}) {
		// If any other block has been accepted then the last synchronous block
		// must have been canonicalised in a previous initialisation.
		return nil
	}

	h := block.Hash()
	b := db.NewBatch()
	rawdb.WriteCanonicalHash(b, h, num)
	block.SetAsHeadBlock(b)
	rawdb.WriteFinalizedBlockHash(b, h)
	return b.Write()
}

// signalNewTxsToEngine subscribes to the [txpool.TxPool] to unblock
// [VM.WaitForEvent] when necessary. [VM.Shutdown] MUST be called to release a
// goroutine started by this method.
func (vm *VM) signalNewTxsToEngine() {
	ch := make(chan core.NewTxsEvent)
	sub := vm.mempool.Pool.SubscribeTransactions(ch, false /*reorgs but ignored by legacypool*/)
	vm.toClose = append(vm.toClose, closerFunc(func() error {
		defer close(ch)
		sub.Unsubscribe()
		return <-sub.Err() // guaranteed to be closed due to unsubscribing
	}))

	// See [VM.WaitForEvent] for why this requires a buffer.
	vm.newTxs = make(chan struct{}, 1)
	go func() {
		defer close(vm.newTxs)
		for range ch {
			select {
			case vm.newTxs <- struct{}{}:
				_ = 0 // coverage visualisation
			default:
				_ = 0 // coverage visualization
			}
		}
	}()
}

// WaitForEvent returns immediately if there are already pending transactions in
// the mempool, otherwise it blocks until the mempool notifies it of new
// transactions. In both cases it returns [snowcommon.PendingTxs]. In the latter
// scenario it respects context cancellation.
func (vm *VM) WaitForEvent(ctx context.Context) (snowcommon.Message, error) {
	if vm.consensusState.Get() == snow.StateSyncing {
		select {
		case err := <-vm.stateSyncDone:
			return snowcommon.StateSyncDone, err
		case <-ctx.Done():
			return 0, context.Cause(ctx)
		}
	}

	if vm.numPendingTxs() > 0 {
		select {
		case <-vm.newTxs: // probably has something buffered
		default:
		}
		return snowcommon.PendingTxs, nil
	}

	// Sends on the `newTxs` channel are performed on a best-effort basis, which
	// could race here if it weren't for the channel buffer.

	for {
		select {
		case _, ok := <-vm.newTxs:
			if !ok {
				return 0, errors.New("VM closed")
			}
			if vm.numPendingTxs() > 0 {
				return snowcommon.PendingTxs, nil
			}

		case <-ctx.Done():
			return 0, context.Cause(ctx)
		}
	}
}

func (vm *VM) numPendingTxs() int {
	p, _ := vm.mempool.Pool.Stats()
	return p
}

// SetState notifies the VM of a transition in the state lifecycle.
func (vm *VM) SetState(ctx context.Context, state snow.State) error {
	vm.consensusState.Set(state)
	return nil
}

// Shutdown gracefully closes the VM.
func (vm *VM) Shutdown(context.Context) error {
	return vm.close()
}

func (vm *VM) close() error {
	errs := make([]error, len(vm.toClose))
	for i, c := range slices.Backward(vm.toClose) {
		errs[i] = c.Close()
	}
	return errors.Join(errs...)
}

// Version reports the VM's version.
func (*VM) Version(context.Context) (string, error) {
	return version.Current.String(), nil
}

func (vm *VM) log() logging.Logger {
	return vm.snowCtx.Log
}
