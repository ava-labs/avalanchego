// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/state/snapshot"
	"github.com/ava-labs/libevm/core/txpool"
	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/event"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/triedb"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/unwind"
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

// directory that stores execution results database under the chain data directory
const executionResultsDir = "sae_execution_results"

func ExecutionResultsPath(chainDataDir string) string {
	return filepath.Join(chainDataDir, executionResultsDir)
}

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

	// closers are closed in reverse order during [VM.Shutdown]. If a resource
	// depends on another resource, it MUST be added AFTER the resource it
	// depends on.
	closers unwind.Closers
}

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
	var closers unwind.Closers
	defer closers.CloseIfPointsToNonNil(&retErr)

	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	snowCtx.Log.Info("creating VM",
		zap.Reflect("config", cfg),
	)

	// ==========  Metrics  ==========
	reg, err := apimetrics.MakeAndRegister(snowCtx.Metrics, "sae")
	if err != nil {
		return nil, fmt.Errorf("registering sae metrics: %w", err)
	}
	metrics, err := newMetrics(reg)
	if err != nil {
		return nil, fmt.Errorf("registering sae metrics: %w", err)
	}

	// ==========  Execution Results DB  ==========
	xdbDir := ExecutionResultsPath(snowCtx.ChainDataDir)
	xdb, err := hooks.ExecutionResultsDB(xdbDir)
	if err != nil {
		return nil, fmt.Errorf("%T.ExecutionResultsDB(%q): %w", hooks, xdbDir, err)
	}
	closers.Push(&xdb)

	// ==========  Block State  ==========
	exec, consensusCritical, err := recoverExecutor(ctx, db, xdb, chainConfig, snowCtx, hooks, cfg, reg)
	if err != nil {
		return nil, fmt.Errorf("creating new execution: %w", err)
	}
	closers.Push(unwind.CloserFunc(func() error {
		exec.Close()
		return exec.Tracker.Close(exec.LastExecuted().SettledStateRoot())
	}))

	// ==========  Mempool & P2P Gossip  ==========
	pool, mempoolClosers, err := newGossipMempool(cfg.MempoolConfig, snowCtx, network, exec, ethBlockSource(consensusCritical, db), reg)
	if err != nil {
		return nil, err
	}
	newTxs, newTxsCloser := signalNewTxsToEngine(pool)
	closers.Push(append(mempoolClosers, newTxsCloser)...)

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
		closers: closers,
	}

	// ==========  Frontiers  ==========
	{
		e := exec.LastExecuted()
		vm.preference.Store(e)
		vm.last.accepted.Store(e)

		s := e.LastSettled()
		vm.last.settled.Store(s)
		metrics.markSettled(s.Height())
	}

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
		vm.closers.Push(rpcProvider)
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
	config legacypool.Config,
	snowCtx *snow.Context,
	network *network.Network,
	exec *saexec.Executor,
	blockSource saetypes.BlockSource,
	reg prometheus.Registerer,
) (_ *txgossip.Set, _ []io.Closer, retErr error) {
	var closers unwind.Closers
	defer closers.CloseIfPointsToNonNil(&retErr)

	bc := txgossip.NewBlockChain(exec, blockSource)
	pools := []txpool.SubPool{
		legacypool.New(config, bc),
	}
	txPool, err := txpool.New(0, bc, pools)
	if err != nil {
		return nil, nil, fmt.Errorf("txpool.New(...): %v", err)
	}
	closers.Push(txPool)

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
	closers.Push(unwind.CloserFunc(func() error {
		cancel()
		wg.Wait()
		return nil
	}))
	return mempool, closers, nil
}

// signalNewTxsToEngine subscribes to the mempool's [txpool.TxPool] to unblock
// [VM.WaitForEvent] when necessary. The returned channel, on which the
// subscription's events are signalled, becomes [VM.newTxs]; the returned
// closer releases the subscription and the goroutine started by this
// function, and MUST be closed.
func signalNewTxsToEngine(mempool *txgossip.Set) (chan struct{}, io.Closer) {
	ch := make(chan core.NewTxsEvent)
	sub := mempool.Pool.SubscribeTransactions(ch, false /*reorgs but ignored by legacypool*/)
	closer := unwind.CloserFunc(func() error {
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

// EVMState returns direct access to the databases that control the EVM state.
func (vm *VM) EVMState() (*triedb.Database, *snapshot.Tree) {
	return vm.exec.TrieDB(), vm.exec.Snapshot()
}

// SetState notifies the VM of a transition in the state lifecycle.
func (vm *VM) SetState(ctx context.Context, state snow.State) error {
	vm.consensusState.Set(state)
	return nil
}

// Shutdown gracefully closes the VM.
func (vm *VM) Shutdown(context.Context) error {
	return vm.closers.Close()
}

// Version reports the VM's version.
func (*VM) Version(context.Context) (string, error) {
	return version.Current.String(), nil
}

func (vm *VM) log() logging.Logger {
	return vm.snowCtx.Log
}
