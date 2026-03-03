// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow"
	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/libevm/accounts"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/txpool"
	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/triedb"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/strevm/blocks"
	"github.com/ava-labs/strevm/hook"
	"github.com/ava-labs/strevm/saedb"
	"github.com/ava-labs/strevm/saexec"
	"github.com/ava-labs/strevm/txgossip"
)

// VM implements all of [adaptor.ChainVM] except for the `Initialize` method,
// which needs to be provided by a harness. In all cases, the harness MUST
// provide a last-synchronous block, which MAY be the genesis.
type VM struct {
	*p2p.Network
	peers *p2p.Peers

	hooks   hook.Points
	config  Config
	snowCtx *snow.Context
	metrics *prometheus.Registry

	db     ethdb.Database
	xdb    saedb.ExecutionResults
	blocks *syncMap[common.Hash, *blocks.Block]

	consensusState utils.Atomic[snow.State]
	preference     atomic.Pointer[blocks.Block]
	last           struct {
		accepted, settled atomic.Pointer[blocks.Block]
		synchronous       uint64
	}

	exec         *saexec.Executor
	mempool      *txgossip.Set
	blockBuilder *blockBuilder
	apiBackend   *ethAPIBackend
	newTxs       chan struct{}

	// toClose are closed in reverse order during [VM.Shutdown]. If a resource
	// depends on another resource, it MUST be added AFTER the resource it
	// depends on.
	toClose [](func() error)
}

// A Config configures construction of a new [VM].
type Config struct {
	MempoolConfig legacypool.Config
	RPCConfig     RPCConfig
	TrieDBConfig  *triedb.Config

	ExcessAfterLastSynchronous gas.Gas

	Now func() time.Time // defaults to [time.Now] if nil
}

// RPCConfig provides options for initialization of RPCs for the node.
type RPCConfig struct {
	BlocksPerBloomSection uint64
	EnableDBInspecting    bool
	EnableProfiling       bool
	DisableTracing        bool
	EVMTimeout            time.Duration
	GasCap                uint64
	TxFeeCap              float64 // 0 = no cap
}

// NewVM returns a new [VM] that is ready for use immediately upon return.
// [VM.Shutdown] MUST be called to release resources.
//
// The state root of the last synchronous block MUST be available when creating
// a [triedb.Database] from the provided [ethdb.Database] and [triedb.Config]
// (the latter provided via the [Config]).
func NewVM(
	ctx context.Context,
	hooks hook.Points,
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
		hooks:   hooks,
		config:  cfg,
		snowCtx: snowCtx,
		metrics: prometheus.NewRegistry(),
		db:      db,
		blocks:  newSyncMap[common.Hash, *blocks.Block](),
	}
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, vm.close())
		}
	}()

	if err := snowCtx.Metrics.Register("sae", vm.metrics); err != nil {
		return nil, err
	}

	xdb, err := hooks.ExecutionResultsDB(
		filepath.Join(snowCtx.ChainDataDir, "sae_execution_results"),
	)
	if err != nil {
		return nil, fmt.Errorf("%T.ExecutionResultsDB(%q): %v", hooks, snowCtx.ChainDataDir, err)
	}
	vm.xdb = xdb
	vm.toClose = append(vm.toClose, xdb.Close)

	lastSync, err := blocks.New(lastSynchronous, nil, nil, snowCtx.Log)
	if err != nil {
		return nil, fmt.Errorf("blocks.New([last synchronous], ...): %v", err)
	}
	vm.last.synchronous = lastSync.Height()

	{ // ==========  Sync -> Async  ==========
		if err := lastSync.MarkSynchronous(hooks, db, xdb, cfg.ExcessAfterLastSynchronous); err != nil {
			return nil, fmt.Errorf("%T{genesis}.MarkSynchronous(): %v", lastSync, err)
		}
		if err := canonicaliseLastSynchronous(db, lastSync); err != nil {
			return nil, err
		}
	}

	rec := &recovery{db, xdb, chainConfig, snowCtx.Log, hooks, cfg, lastSync}
	{ // ==========  Executor  ==========
		lastExecuted, unexecuted, err := rec.recoverFromDB()
		if err != nil {
			return nil, err
		}

		exec, err := saexec.New(
			lastExecuted,
			vm.headerSource,
			chainConfig,
			db,
			xdb,
			cfg.TrieDBConfig,
			hooks,
			snowCtx.Log,
		)
		if err != nil {
			return nil, fmt.Errorf("saexec.New(...): %v", err)
		}
		vm.exec = exec
		vm.toClose = append(vm.toClose, exec.Close)

		last := lastExecuted
		for b, err := range unexecuted {
			if err != nil {
				return nil, err
			}
			if err := exec.Enqueue(ctx, b); err != nil {
				return nil, err
			}
			last = b
		}
		if err := last.WaitUntilExecuted(ctx); err != nil {
			return nil, err
		}
	}

	{ // ==========  Blocks in memory  ==========
		head := vm.exec.LastExecuted()

		bMap, lastSettled, err := rec.rebuildBlocksInMemory(head)
		if err != nil {
			return nil, err
		}
		vm.blocks = bMap

		vm.last.settled.Store(lastSettled)
		vm.last.accepted.Store(head)
		vm.preference.Store(head)
	}

	{ // ==========  Mempool  ==========
		bc := txgossip.NewBlockChain(vm.exec, vm.ethBlockSource)
		pools := []txpool.SubPool{
			legacypool.New(cfg.MempoolConfig, bc),
		}
		txPool, err := txpool.New(0, bc, pools)
		if err != nil {
			return nil, fmt.Errorf("txpool.New(...): %v", err)
		}
		vm.toClose = append(vm.toClose, txPool.Close)

		metrics, err := bloom.NewMetrics("mempool", vm.metrics)
		if err != nil {
			return nil, err
		}
		conf := gossip.BloomSetConfig{Metrics: metrics}
		pool, err := txgossip.NewSet(snowCtx.Log, txPool, conf)
		if err != nil {
			return nil, err
		}
		vm.mempool = pool
		vm.signalNewTxsToEngine()
	}

	{ // ==========  Block Builder  ==========
		vm.blockBuilder = &blockBuilder{
			hooks,
			cfg.Now,
			snowCtx.Log,
			vm.exec,
			vm.mempool,
		}
	}

	{ // ==========  P2P Gossip  ==========
		network, peers, validatorPeers, err := newNetwork(snowCtx, sender, vm.metrics)
		if err != nil {
			return nil, fmt.Errorf("newNetwork(...): %v", err)
		}

		const pullGossipPeriod = time.Second
		handler, pullGossiper, pushGossiper, err := gossip.NewSystem(
			snowCtx.NodeID,
			network,
			validatorPeers,
			vm.mempool,
			txgossip.Marshaller{},
			gossip.SystemConfig{
				Log:           snowCtx.Log,
				Registry:      vm.metrics,
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

		vm.Network = network
		vm.peers = peers
		vm.mempool.RegisterPushGossiper(pushGossiper)
		vm.toClose = append(vm.toClose, func() error {
			cancel()
			wg.Wait()
			return nil
		})
	}

	{ // ==========  API Backend  ==========
		// Empty account manager provides graceful errors for signing
		// RPCs (e.g. eth_sign) instead of nil-pointer panics. No
		// actual account functionality is expected.
		accountManager := accounts.NewManager(&accounts.Config{})
		vm.toClose = append(vm.toClose, accountManager.Close)

		chainIdx := chainIndexer{vm.exec}
		override := bloomOverrider{vm.db}
		// TODO(alarso16): if we are state syncing, we need to provide the first
		// block available to the indexer via [core.ChainIndexer.AddCheckpoint].
		bloomIdx := newBloomIndexer(vm.db, chainIdx, override, cfg.RPCConfig.BlocksPerBloomSection)
		vm.toClose = append(vm.toClose, bloomIdx.Close)

		vm.apiBackend = &ethAPIBackend{
			vm:             vm,
			accountManager: accountManager,
			Set:            vm.mempool,
			chainIndexer:   chainIdx,
			bloomIndexer:   bloomIdx,
			bloomOverrider: override,
		}
	}

	return vm, nil
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
	vm.toClose = append(vm.toClose, func() error {
		defer close(ch)
		sub.Unsubscribe()
		return <-sub.Err() // guaranteed to be closed due to unsubscribing
	})

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
	for i, fn := range slices.Backward(vm.toClose) {
		errs[i] = fn()
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
