// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package cchain implements the C-Chain VM atop [sae.VM]. It composes the
// C-Chain block-building hooks, the cross-chain transaction pool, and the avax
// JSON-RPC service that ingests Export and Import transactions.
package cchain

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/ava-labs/libevm/triedb"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/evm/utils/rpc"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/state"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/statesync"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/txpool"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp"
	"github.com/ava-labs/avalanchego/vms/saevm/libevmlog"
	"github.com/ava-labs/avalanchego/vms/saevm/network"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
	"github.com/ava-labs/avalanchego/vms/saevm/types"

	apimetrics "github.com/ava-labs/avalanchego/api/metrics"
	avadb "github.com/ava-labs/avalanchego/database"
	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	ethparams "github.com/ava-labs/libevm/params"
)

var _ adaptor.ChainVM[*blocks.Block] = (*VM)(nil)

// VM wraps an [sae.VM] with the cross-chain pieces specific to the C-Chain.
type VM struct {
	*sae.VM            // created by [VM.SetState] as bootstrapping or normal op
	*network.Network   // created by [VM.Initialize]
	*statesync.Handler // created by [VM.Initialize]

	// gossip frequencies are configurable to speed up testing.
	pullGossipPeriod time.Duration
	pushGossipPeriod time.Duration

	// now is the clock provided to the [sae.VM] and is used for block building.
	now              func() time.Time
	lastWaitForEvent utils.Atomic[time.Time]

	chainConfig *ethparams.ChainConfig
	state       *state.State
	metrics     *metrics
	pending     *txpool.Pending

	// TODO(alarso16): Remove from VM - only referenced in tests.
	gossipSet *gossip.BloomSet[*gossipTx]

	mode                 utils.Atomic[snow.State]
	finishInitialize     func(context.Context) error
	finishInitializeOnce sync.Once
	handlers             *api.MutableHTTPHandlers

	closeMu sync.Mutex
	onClose []func(context.Context) error // called in reverse order on shutdown
	closed  bool
}

var (
	ethDBPrefix      = []byte("ethdb")
	errAlreadyClosed = errors.New("already closed")
)

// Initialize initializes the VM.
func (vm *VM) Initialize(
	ctx context.Context,
	snowCtx *snow.Context,
	avaDB avadb.Database,
	genesisBytes []byte,
	_ []byte,
	configBytes []byte,
	_ []*snowcommon.Fx,
	appSender snowcommon.AppSender,
) (retErr error) {
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, vm.Shutdown(ctx))
		}
	}()

	vm.closeMu.Lock() // [VM.Shutdown] acquires this lock.
	defer vm.closeMu.Unlock()
	if vm.closed {
		return errAlreadyClosed
	}

	userConfig, err := parseConfig(configBytes, snowCtx.NetworkID)
	if err != nil {
		return fmt.Errorf("parsing user config: %w", err)
	}
	if lvl := userConfig.LogLevel; lvl != nil {
		snowCtx.Log.SetLevel(*lvl)
	}
	// libevm's default logger discards everything, hiding snapshot lifecycle
	// events (load failures, rebuilds, generation progress) among others.
	libevmlog.Route(snowCtx.Log)
	snowCtx.Log.Info("initializing C-Chain",
		zap.Reflect("config", userConfig),
	)

	warpMessages, err := userConfig.WarpMessages()
	if err != nil {
		return fmt.Errorf("parsing warp messages: %w", err)
	}

	genesis, err := parseGenesis(snowCtx, genesisBytes)
	if err != nil {
		return fmt.Errorf("parsing genesis: %w", err)
	}
	vm.chainConfig = genesis.Config

	vm.state, err = state.New(snowCtx, avaDB)
	if err != nil {
		return fmt.Errorf("creating cchain state: %w", err)
	}
	vm.onClose = append(vm.onClose, func(context.Context) error {
		return vm.state.Close()
	})

	reg, err := apimetrics.MakeAndRegister(snowCtx.Metrics, "cchain")
	if err != nil {
		return fmt.Errorf("making metrics: %w", err)
	}
	vm.metrics, err = newMetrics(reg)
	if err != nil {
		return fmt.Errorf("registering cchain metrics: %w", err)
	}

	vm.pending = txpool.NewPending()
	warpStorage := warp.NewStorage(avaDB, warpMessages...)
	hooks := newHooks(
		snowCtx,
		vm.state,
		vm.chainConfig,
		vm.pending,
		warpStorage,
		vm.now,
		userConfig.desired(),
		vm.metrics,
	)

	vm.Network, err = network.New(snowCtx, appSender, userConfig.networkOptions()...)
	if err != nil {
		return fmt.Errorf("creating network: %w", err)
	}

	// [prefixdb.NewNested] is used because coreth used to be run as a plugin.
	// This meant that the database's prefix was not compacted, because the
	// provided database was wrapped by the rpcchainvm.
	ethDB := types.NewEthDB(prefixdb.NewNested(ethDBPrefix, avaDB))

	if err := genesis.verifyAndWriteBlock(ethDB); err != nil {
		return fmt.Errorf("writing genesis block: %w", err)
	}
	vm.Handler, err = statesync.New(
		userConfig.stateSyncConfig(),
		ethDB,
		snowCtx,
		vm.Network,
		hooks,
		vm.state,
	)
	if err != nil {
		return fmt.Errorf("creating summary handler: %w", err)
	}
	vm.onClose = append(vm.onClose, vm.Handler.Shutdown)
	vm.handlers = api.NewMutableHTTPHandlers(handlerPaths...)

	// [VM.finishInitialize] adds the [sae.VM] after all necessary state is available.
	// This MUST be called exactly once, guaranteed using [VM.finishInitializeOnce].
	vm.finishInitialize = func(ctx context.Context) error {
		vm.closeMu.Lock()
		defer vm.closeMu.Unlock()
		if vm.closed {
			return errAlreadyClosed
		}

		saeConfig := userConfig.saeConfig(vm.now)
		tdbConfig := saeConfig.DBConfig.TrieDBConfig(snowCtx.ChainDataDir, snowCtx.Log)
		if err := genesis.setupTrieDB(ethDB, tdbConfig); err != nil {
			return fmt.Errorf("setting up genesis trie: %w", err)
		}

		// Uses of [sae.VM] are NOT protected by [VM.closeMu]. However,
		// [VM.activeHandler] ensures that methods accessing [sae.VM] only occur
		// AFTER [vm.SetState] is called with [snow.Bootstrapping] or
		// [snow.NormalOp], which guarantees that this method has returned no error.
		var err error
		vm.VM, err = sae.NewVM(ctx, hooks, saeConfig, snowCtx, vm.chainConfig, ethDB, vm.Network)
		if err != nil {
			return fmt.Errorf("creating SAE VM: %w", err)
		}
		vm.onClose = append(vm.onClose, vm.VM.Shutdown)

		const maxTxPoolSize = 1024
		txpool, err := txpool.New(snowCtx, vm.chainConfig, vm.pending, vm.VM, maxTxPoolSize)
		if err != nil {
			return fmt.Errorf("creating txpool: %w", err)
		}
		vm.onClose = append(vm.onClose, func(context.Context) error {
			txpool.Close()
			return nil
		})

		bloomMetrics, err := bloom.NewMetrics("gossip_bloom", reg)
		if err != nil {
			return fmt.Errorf("creating gossip bloom metrics: %w", err)
		}
		vm.gossipSet, err = gossip.NewBloomSet(
			newGossipTxPool(txpool),
			gossip.BloomSetConfig{
				Metrics: bloomMetrics,
			},
		)
		if err != nil {
			return fmt.Errorf("creating gossip bloom set: %w", err)
		}

		gossipHandler, pullGossiper, pushGossiper, err := gossip.NewSystem(
			snowCtx.NodeID,
			vm.Network.Network,
			vm.Network.ValidatorPeers,
			vm.gossipSet,
			gossipMarshaller{},
			gossip.SystemConfig{
				Log:           snowCtx.Log,
				Registry:      reg,
				Namespace:     "gossip",
				HandlerID:     p2p.AtomicTxGossipHandlerID,
				RequestPeriod: vm.pullGossipPeriod,
			},
		)
		if err != nil {
			return fmt.Errorf("creating cross-chain tx gossip system: %w", err)
		}

		// RPC handlers
		{
			m, err := vm.VM.CreateHandlers(ctx)
			if err != nil {
				return fmt.Errorf("creating SAE handlers: %w", err)
			}
			service, err := newService(snowCtx, vm.gossipSet, pushGossiper, vm.state)
			if err != nil {
				return fmt.Errorf("creating avax service: %w", err)
			}
			handler, err := rpc.NewHandler(avaxServiceName, service)
			if err != nil {
				return fmt.Errorf("creating avax RPC handler: %w", err)
			}
			m[avaxHTTPExtensionPath] = handler
			vm.handlers.Set(m)
		}

		// Start gossip
		{
			if err := vm.Network.AddHandler(p2p.AtomicTxGossipHandlerID, gossipHandler); err != nil {
				return fmt.Errorf("registering cross-chain tx gossip handler: %w", err)
			}

			gossipCtx, cancelGossip := context.WithCancel(context.Background())
			var gossipWG sync.WaitGroup
			gossipWG.Go(func() {
				gossip.Every(gossipCtx, snowCtx.Log, pullGossiper, vm.pullGossipPeriod)
			})
			gossipWG.Go(func() {
				gossip.Every(gossipCtx, snowCtx.Log, pushGossiper, vm.pushGossipPeriod)
			})
			vm.onClose = append(vm.onClose, func(context.Context) error {
				cancelGossip()
				gossipWG.Wait()
				return nil
			})
			if err := registerWarpHandler(vm.VM, vm.Network, warpStorage, snowCtx.WarpSigner); err != nil {
				return fmt.Errorf("registering warp signature handler: %w", err)
			}
		}

		// Register state sync server
		{
			// TODO(alarso16): Find a way to wire in Firewood.
			if saeConfig.DBConfig.Scheme != customrawdb.FirewoodScheme {
				// The triedb shouldn't share a cache with execution.
				tdb := triedb.NewDatabase(ethDB, tdbConfig)
				_, snaps := vm.VM.EVMState()
				if err := vm.Handler.RegisterServer(tdb, snaps); err != nil {
					return fmt.Errorf("registering state sync server: %w", err)
				}
			}
		}
		return nil
	}

	return nil
}

const (
	avaxServiceName       = "avax"
	avaxHTTPExtensionPath = "/" + avaxServiceName
)

var handlerPaths = append(slices.Clone(sae.HandlerPaths), avaxHTTPExtensionPath)

// CreateHandlers returns the HTTP handlers exposed by the underlying SAE VM
// augmented with the avax service. None of the handlers are usable until after
// the [VM] is set as bootstrapping/normal operation.
func (vm *VM) CreateHandlers(context.Context) (map[string]http.Handler, error) {
	return vm.handlers.AsInterface(), nil
}

// SetState sets the state of the VM. If the state is transitioning to
// [snow.Bootstrapping], the full VM will be initialized. Any error returned
// is fatal.
func (vm *VM) SetState(ctx context.Context, state snow.State) error {
	if state >= snow.Bootstrapping {
		var err error
		vm.finishInitializeOnce.Do(func() {
			if err = vm.Handler.Error(); err != nil {
				return
			}
			err = vm.finishInitialize(ctx)
		})
		if err != nil {
			return err
		}

		if err := vm.VM.SetState(ctx, state); err != nil {
			return fmt.Errorf("setting sae.VM state: %w", err)
		}
	}

	// MUST occur after [VM.prepBlockHandling] to avoid race setting [VM.VM].
	vm.mode.Set(state)
	return nil
}

var (
	_ stateDependent = (*sae.VM)(nil)
	_ stateDependent = (*statesync.Handler)(nil)
)

type stateDependent interface {
	ParseBlock(context.Context, []byte) (*blocks.Block, error)
	GetBlock(context.Context, ids.ID) (*blocks.Block, error)
	GetBlockIDAtHeight(context.Context, uint64) (ids.ID, error)
	LastAccepted(context.Context) (ids.ID, error)
}

func (vm *VM) activeHandler() stateDependent {
	if vm.mode.Get() >= snow.Bootstrapping {
		return vm.VM
	}
	return vm.Handler
}

// ParseBlock parses a block from bytes.
func (vm *VM) ParseBlock(ctx context.Context, blockBytes []byte) (*blocks.Block, error) {
	return vm.activeHandler().ParseBlock(ctx, blockBytes)
}

// GetBlock returns the [blocks.Block] with the given ID.
func (vm *VM) GetBlock(ctx context.Context, id ids.ID) (*blocks.Block, error) {
	return vm.activeHandler().GetBlock(ctx, id)
}

// GetBlockIDAtHeight returns the ID of the block at the given height.
func (vm *VM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	return vm.activeHandler().GetBlockIDAtHeight(ctx, height)
}

// LastAccepted returns the ID of the last accepted block.
func (vm *VM) LastAccepted(ctx context.Context) (ids.ID, error) {
	return vm.activeHandler().LastAccepted(ctx)
}

// earliestBuildTime returns the earliest wall-clock time at which a child of b
// may be built.
func earliestBuildTime(b *blocks.Block) time.Time {
	h := b.Header()
	return blockTime(h).Add(delayExponent(h).DelayDuration())
}

// minWaitForEventDelay is the minimum spacing between consecutive
// [VM.WaitForEvent] returns. 100ms isn't special here, it was selected as a
// reasonable frequency for the engine to poll on whether to build a block or
// not.
const minWaitForEventDelay = 100 * time.Millisecond

// WaitForEvent waits until the ACP-226 minimum block delay since the preferred
// block has elapsed, then waits for a transaction to be in the txpool or for
// the SAE VM to produce an event.
func (vm *VM) WaitForEvent(ctx context.Context) (snowcommon.Message, error) {
	switch vm.mode.Get() {
	case snow.Initializing:
		// no event can occur while the VM is initializing.
		<-ctx.Done()
		return 0, context.Cause(ctx)
	case snow.StateSyncing:
		return vm.Handler.WaitForEvent(ctx)
	}

	// Throttle to avoid busy looping: the txpools only clear after block
	// execution, so pending txs can re-signal while their block is processing.
	//
	// TODO(JonathanOppenheimer): The txpool should track preference / reorgs so
	// we don't need this throttle.
	throttleUntil := vm.lastWaitForEvent.Get().Add(minWaitForEventDelay)

	// Pace block building on the ACP-226 minimum block delay so that the event
	// sources are consulted when we are actually willing to build.
	buildTime := earliestBuildTime(vm.VM.GetPreference())

	until := throttleUntil
	if buildTime.After(until) {
		until = buildTime
	}
	if err := vm.waitUntil(ctx, until); err != nil {
		return 0, err
	}

	// Race the SAE event source against the cross-chain txpool. The winner's
	// deferred cancel unblocks the loser, whose pending call returns and delivers
	// its discarded result to the buffered channel, so neither goroutine leaks.
	raceCtx, cancel := context.WithCancel(ctx)
	type result struct {
		msg snowcommon.Message
		err error
	}
	results := make(chan result, 2)
	go func() {
		defer cancel()
		msg, err := vm.VM.WaitForEvent(raceCtx)
		results <- result{msg, err}
	}()
	go func() {
		defer cancel()
		err := vm.pending.AwaitTxs(raceCtx)
		results <- result{snowcommon.PendingTxs, err}
	}()

	r := <-results
	if r.err == nil {
		vm.lastWaitForEvent.Set(vm.now())
	}
	return r.msg, r.err
}

// waitUntil blocks until [VM.now] reaches t, returning early with the
// cancellation cause if ctx is canceled first.
func (vm *VM) waitUntil(ctx context.Context, t time.Time) error {
	timeToWait := t.Sub(vm.now())
	if timeToWait <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-time.After(timeToWait):
		return nil
	}
}

// Shutdown releases every resource allocated by [VM.Initialize] in reverse
// order.
//
// It is idempotent and safe to call after a partially-failed [VM.Initialize].
func (vm *VM) Shutdown(ctx context.Context) error {
	vm.closeMu.Lock()
	defer vm.closeMu.Unlock()
	if vm.closed {
		return nil
	}
	vm.closed = true

	errs := make([]error, len(vm.onClose))
	for i, f := range slices.Backward(vm.onClose) {
		errs[i] = f(ctx)
	}
	vm.onClose = nil
	return errors.Join(errs...)
}
