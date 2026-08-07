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
	"github.com/ava-labs/libevm/core/txpool"
	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/event"
	"github.com/ava-labs/libevm/params"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/network"
	"github.com/ava-labs/avalanchego/vms/saevm/sae/rpc"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
	"github.com/ava-labs/avalanchego/vms/saevm/saexec"
	"github.com/ava-labs/avalanchego/vms/saevm/txgossip"

	apimetrics "github.com/ava-labs/avalanchego/api/metrics"
	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

// VM implements all of [adaptor.ChainVM] except for the `Initialize` method,
// which needs to be provided by a harness. In all cases, the harness MUST
// ensure that the last-synchronous block (which MAY be the genesis) is
// canonical on disk with its post-execution state committed before [NewVM] is
// called.
type VM struct {
	network *network.Network
	hooks   hook.Points
	config  Config
	snowCtx *snow.Context
	metrics *metrics

	db  ethdb.Database
	xdb saetypes.ExecutionResults

	consensusState utils.Atomic[snow.State]

	preference     atomic.Pointer[blocks.Block]
	last           last
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
//
// TODO(JonathanOppenheimer): add a Verify method that checks all sub-configs
// (e.g. [rpc.Config.Verify]) and call it from [NewVM] so the VM doesn't
// assume its caller validated the config.
type Config struct {
	MempoolConfig legacypool.Config
	DBConfig      saedb.Config
	RPCConfig     rpc.Config

	// Now defaults to [time.Now] if nil
	Now func() time.Time `json:"-"`
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
	network *network.Network,
) (_ *VM, retErr error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	snowCtx.Log.Info("creating VM",
		zap.Reflect("config", cfg),
	)

	reg, err := apimetrics.MakeAndRegister(snowCtx.Metrics, "sae")
	if err != nil {
		return nil, fmt.Errorf("registering sae metrics: %w", err)
	}
	metrics, err := newMetrics(reg)
	if err != nil {
		return nil, fmt.Errorf("registering sae metrics: %w", err)
	}

	var toClose []io.Closer
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, closeInReverse(toClose))
		}
	}()

	xdb, err := hooks.ExecutionResultsDB(
		filepath.Join(snowCtx.ChainDataDir, "sae_execution_results"),
	)
	if err != nil {
		return nil, fmt.Errorf("%T.ExecutionResultsDB(%q): %v", hooks, snowCtx.ChainDataDir, err)
	}
	toClose = append(toClose, &xdb)

	var blockState *blockStateFields
	{ // ==========  Block State  ==========
		rec := &recovery{db, xdb, chainConfig, snowCtx, hooks, cfg}
		lastCommitted, err := rec.lastCommittedBlock()
		if err != nil {
			return nil, fmt.Errorf("finding last committed state: %w", err)
		}

		tr, err := saedb.NewTracker(
			db,
			cfg.DBConfig,
			lastCommitted.PostExecutionStateRoot(),
			snowCtx.ChainDataDir,
			snowCtx.Log,
		)
		if err != nil {
			return nil, fmt.Errorf("saedb.NewTracker(...): %v", err)
		}
		bMap := newSyncMap[common.Hash, *blocks.Block](
			func(b *blocks.Block) {
				tr.Track(b.SettledStateRoot())
				// The post-execution root is tracked by the [saexec.Executor] as
				// soon as it's known. In the case of database recovery, this
				// occurred in [recovery.executeAllAccepted].
			},
			func(b *blocks.Block) {
				tr.Untrack(b.SettledStateRoot())
				if b.Executed() { // i.e. deleted due to settlement not rejection
					tr.Untrack(b.PostExecutionStateRoot())
				}
			},
		)

		exec, err := saexec.New(
			lastCommitted,
			headerSource(bMap, db),
			chainConfig,
			db,
			xdb,
			tr,
			cfg.DBConfig,
			hooks,
			snowCtx,
			reg,
		)
		if err != nil {
			return nil, fmt.Errorf("saexec.New(...): %v", err)
		}
		toClose = append(toClose, exec)

		if err := rec.executeAllAccepted(ctx, exec); err != nil {
			return nil, fmt.Errorf("executing all previously accepted blocks: %w", err)
		}

		lastSettled, err := rec.populateConsensusCriticalBlocks(exec, bMap)
		if err != nil {
			return nil, fmt.Errorf("finding consensus-critical blocks: %w", err)
		}

		head := exec.LastExecuted()
		metrics.markSettled(lastSettled.Height())

		blockState = &blockStateFields{
			exec,
			bMap,
			atomicPointerTo(head),
			last{ //exhaustruct:enforce
				accepted: atomicPointerTo(head),
				settled:  atomicPointerTo(lastSettled),
			},
		}
	}

	var mempool *txgossip.Set
	{ // ==========  Mempool  ==========
		bc := txgossip.NewBlockChain(blockState.exec, ethBlockSource(blockState.consensusCritical, db))
		pools := []txpool.SubPool{
			legacypool.New(cfg.MempoolConfig, bc),
		}
		txPool, err := txpool.New(0, bc, pools)
		if err != nil {
			return nil, fmt.Errorf("txpool.New(...): %v", err)
		}
		toClose = append(toClose, txPool)

		bloomMetrics, err := bloom.NewMetrics("mempool", reg)
		if err != nil {
			return nil, err
		}
		conf := gossip.BloomSetConfig{Metrics: bloomMetrics}
		pool, err := txgossip.NewSet(blockState.exec, txPool, conf)
		if err != nil {
			return nil, err
		}
		mempool = pool
	}

	// ==========  Block builder  ==========
	blockBuilder := &blockBuilderG[T]{
		hooks,
		cfg.Now,
		snowCtx.Log,
		blockState.exec,
		mempool,
		ethBlockSource(blockState.consensusCritical, db),
	}

	{ // ==========  P2P Gossip  ==========
		const pullGossipPeriod = time.Second
		handler, pullGossiper, pushGossiper, err := gossip.NewSystem(
			snowCtx.NodeID,
			network.Network,
			network.ValidatorPeers,
			mempool,
			txgossip.Marshaller{},
			gossip.SystemConfig{
				Log:           snowCtx.Log,
				Registry:      reg,
				Namespace:     "gossip",
				RequestPeriod: pullGossipPeriod,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("gossip.NewSystem(...): %v", err)
		}
		if err := network.AddHandler(p2p.TxGossipHandlerID, handler); err != nil {
			return nil, fmt.Errorf("network.AddHandler(...): %v", err)
		}

		var (
			gossipCtx, cancel = context.WithCancel(context.Background())
			wg                sync.WaitGroup
		)
		wg.Add(2)
		go func() {
			defer wg.Done()
			gossip.Every(gossipCtx, snowCtx.Log, pullGossiper, pullGossipPeriod)
		}()
		go func() {
			defer wg.Done()
			const pushGossipPeriod = 100 * time.Millisecond
			gossip.Every(gossipCtx, snowCtx.Log, pushGossiper, pushGossipPeriod)
		}()

		mempool.RegisterPushGossiper(pushGossiper)
		toClose = append(toClose, closerFunc(func() error {
			cancel()
			wg.Wait()
			return nil
		}))
	}

	newTxs, cleanup := signalNewTxsToEngine(mempool)
	toClose = append(toClose, cleanup)

	//exhaustruct:enforce
	vm := &VM{
		network:           network,
		hooks:             hooks,
		config:            cfg,
		snowCtx:           snowCtx,
		metrics:           metrics,
		db:                db,
		xdb:               xdb,
		consensusState:    utils.Atomic[snow.State]{},
		preference:        cloneAtomicPointer(&blockState.preference),
		last:              blockState.last.clone(),
		acceptedBlocks:    event.FeedOf[*blocks.Block]{},
		consensusCritical: blockState.consensusCritical,
		exec:              blockState.exec,
		mempool:           mempool,
		blockBuilder:      blockBuilder,
		// TODO(arr4n) there is a circular dependency that isn't necessarily
		// worth untangling: the RPC provider requires the VM as it satisfies
		// part of [rpc.Chain], but the VM requires the provider for creating
		// HTTP handlers.
		rpcProvider: nil,
		newTxs:      newTxs,
		toClose:     toClose,
	}

	{ // ==========  RPC Provider  ==========
		r, err := rpc.New(vm.chain(), cfg.RPCConfig)
		if err != nil {
			return nil, err
		}
		vm.toClose = append(vm.toClose, r)
		vm.rpcProvider = r
	}

	return vm, nil
}

// signalNewTxsToEngine subscribes to the [txpool.TxPool] to unblock
// [VM.WaitForEvent] when necessary. [VM.Shutdown] MUST be called to release a
// goroutine started by this method.
func signalNewTxsToEngine(mempool *txgossip.Set) (chan struct{}, io.Closer) {
	ch := make(chan core.NewTxsEvent)
	sub := mempool.Pool.SubscribeTransactions(ch, false /*reorgs but ignored by legacypool*/)

	// See [VM.WaitForEvent] for why this requires a buffer.
	newTxs := make(chan struct{}, 1)
	go func() {
		defer close(newTxs)
		for range ch {
			select {
			case newTxs <- struct{}{}:
				_ = 0 // coverage visualisation
			default:
				_ = 0 // coverage visualization
			}
		}
	}()
	return newTxs, closerFunc(func() error {
		defer close(ch)
		sub.Unsubscribe()
		return <-sub.Err() // guaranteed to be closed due to unsubscribing
	})
}

// WaitForEvent returns immediately if there are already pending transactions in
// the mempool, otherwise it blocks until the mempool notifies it of new
// transactions. In both cases it returns [snowcommon.PendingTxs]. In the latter
// scenario it respects context cancellation.
func (vm *VM) WaitForEvent(ctx context.Context) (snowcommon.Message, error) {
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

func closeInReverse(cs []io.Closer) error {
	errs := make([]error, len(cs))
	for i, c := range slices.Backward(cs) {
		errs[i] = c.Close()
	}
	return errors.Join(errs...)
}

func (vm *VM) close() error {
	return closeInReverse(vm.toClose)
}

// Version reports the VM's version.
func (*VM) Version(context.Context) (string, error) {
	return version.Current.String(), nil
}

func (vm *VM) log() logging.Logger {
	return vm.snowCtx.Log
}
