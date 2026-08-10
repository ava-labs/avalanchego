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
	"github.com/prometheus/client_golang/prometheus"
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

	preference atomic.Pointer[blocks.Block]
	last       struct {
		accepted, settled atomic.Pointer[blocks.Block]
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
	var toClose []io.Closer
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, closeAll(toClose))
		}
	}()

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

	xdb, err := hooks.ExecutionResultsDB(
		filepath.Join(snowCtx.ChainDataDir, "sae_execution_results"),
	)
	if err != nil {
		return nil, fmt.Errorf("%T.ExecutionResultsDB(%q): %v", hooks, snowCtx.ChainDataDir, err)
	}
	toClose = append(toClose, &xdb)

	// ==========  Block State  ==========
	rec := &recovery{db, xdb, chainConfig, snowCtx, hooks, cfg}
	exec, consensusCritical, err := rec.newExecution(reg)
	if err != nil {
		return nil, fmt.Errorf("creating new execution: %w", err)
	}
	toClose = append(toClose, exec)

	if err := rec.executeAllAccepted(ctx, exec); err != nil {
		return nil, fmt.Errorf("executing all previously accepted blocks: %w", err)
	}

	lastSettled, err := rec.populateConsensusCriticalBlocks(exec, consensusCritical)
	if err != nil {
		return nil, fmt.Errorf("finding consensus-critical blocks: %w", err)
	}

	// ==========  Mempool & P2P Gossip  ==========
	pool, mempoolClosers, err := newGossipMempool(cfg.MempoolConfig, snowCtx, network, exec, ethBlockSource(consensusCritical, db), reg)
	if err != nil {
		return nil, err
	}
	toClose = append(toClose, mempoolClosers...)

	newTxs, newTxsCloser := signalNewTxsToEngine(pool)
	toClose = append(toClose, newTxsCloser)

	// ==========  Metrics  ==========
	metrics, err := newMetrics(reg)
	if err != nil {
		return nil, fmt.Errorf("registering sae metrics: %w", err)
	}
	metrics.markSettled(lastSettled.Height())

	vm := &VM{
		network:           network,
		hooks:             hooks,
		config:            cfg,
		snowCtx:           snowCtx,
		metrics:           metrics,
		db:                db,
		xdb:               xdb,
		consensusCritical: consensusCritical,
		exec:              exec,
		mempool:           pool,
		blockBuilder: &blockBuilderG[T]{
			hooks,
			cfg.Now,
			snowCtx.Log,
			exec,
			pool,
			ethBlockSource(consensusCritical, db),
		},
		newTxs:  newTxs,
		toClose: toClose,
	}

	head := exec.LastExecuted()
	vm.preference.Store(head)
	vm.last.accepted.Store(head)
	vm.last.settled.Store(lastSettled)

	// ==========  RPC Provider  ==========
	{
		// TODO(arr4n) there is a circular dependency that isn't necessarily
		// worth untangling: the RPC provider requires the VM as it satisfies
		// part of [rpc.Chain], but the VM requires the provider for creating
		// HTTP handlers.
		rpcProvider, err := rpc.New(vm.chain(), cfg.RPCConfig)
		if err != nil {
			return nil, err
		}
		vm.toClose = append(vm.toClose, rpcProvider)
		vm.rpcProvider = rpcProvider
	}
	return vm, nil
}

// newGossipMempool creates the mempool, registers it with a new p2p gossip
// system on the network, and starts the pull- and push-gossip loops.
//
// The returned closers MUST all be closed, in reverse order, to release the
// resources and goroutines created by this function; the last closer stops
// the gossip loops, blocking until they have returned.
func newGossipMempool(
	mempoolConfig legacypool.Config,
	snowCtx *snow.Context,
	network *network.Network,
	exec *saexec.Executor,
	blockSource saetypes.BlockSource,
	reg prometheus.Registerer,
) (_ *txgossip.Set, _ []io.Closer, retErr error) {
	var toClose []io.Closer
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, closeAll(toClose))
		}
	}()

	bc := txgossip.NewBlockChain(exec, blockSource)
	pools := []txpool.SubPool{
		legacypool.New(mempoolConfig, bc),
	}
	txPool, err := txpool.New(0, bc, pools)
	if err != nil {
		return nil, nil, fmt.Errorf("txpool.New(...): %v", err)
	}
	toClose = append(toClose, txPool)

	bloomMetrics, err := bloom.NewMetrics("mempool", reg)
	if err != nil {
		return nil, nil, err
	}
	conf := gossip.BloomSetConfig{Metrics: bloomMetrics}
	mempool, err := txgossip.NewSet(exec, txPool, conf)
	if err != nil {
		return nil, nil, err
	}

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
		return nil, nil, fmt.Errorf("gossip.NewSystem(...): %v", err)
	}
	if err := network.AddHandler(p2p.TxGossipHandlerID, handler); err != nil {
		return nil, nil, fmt.Errorf("network.AddHandler(...): %v", err)
	}

	var (
		gossipCtx, cancel = context.WithCancel(context.Background())
		wg                sync.WaitGroup
	)
	wg.Go(func() {
		gossip.Every(gossipCtx, snowCtx.Log, pullGossiper, pullGossipPeriod)
	})
	wg.Go(func() {
		const pushGossipPeriod = 100 * time.Millisecond
		gossip.Every(gossipCtx, snowCtx.Log, pushGossiper, pushGossipPeriod)
	})

	mempool.RegisterPushGossiper(pushGossiper)
	toClose = append(toClose, closerFunc(func() error {
		cancel()
		wg.Wait()
		return nil
	}))
	return mempool, toClose, nil
}

// signalNewTxsToEngine subscribes to the mempool's [txpool.TxPool] to unblock
// [VM.WaitForEvent] when necessary. The returned channel, on which the
// subscription's events are signalled, becomes [VM.newTxs]; the returned
// closer releases the subscription and the goroutine started by this
// function, and MUST be closed.
func signalNewTxsToEngine(mempool *txgossip.Set) (chan struct{}, io.Closer) {
	ch := make(chan core.NewTxsEvent)
	sub := mempool.Pool.SubscribeTransactions(ch, false /*reorgs but ignored by legacypool*/)
	closer := closerFunc(func() error {
		defer close(ch)
		sub.Unsubscribe()
		return <-sub.Err() // guaranteed to be closed due to unsubscribing
	})

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
	return newTxs, closer
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
	return closeAll(vm.toClose)
}

func closeAll(closers []io.Closer) error {
	errs := make([]error, len(closers))
	for i, c := range slices.Backward(closers) {
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
