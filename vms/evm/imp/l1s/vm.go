// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package l1s implements the Subnet-EVM compatible L1 VM atop [sae.VM].
package l1s

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/ava-labs/firewood-go/ffi"
	"github.com/ava-labs/libevm/triedb"
	"go.uber.org/zap"

	// TODO(alarso16): Remove the dependency on the global module registration.
	_ "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/registry" // register precompiles

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/evm/uptimetracker"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/evm/adaptor"
	"github.com/ava-labs/avalanchego/vms/evm/blocks"
	"github.com/ava-labs/avalanchego/vms/evm/firewood"
	"github.com/ava-labs/avalanchego/vms/evm/imp/blockdelay"
	"github.com/ava-labs/avalanchego/vms/evm/imp/l1s/hookadaptor"
	"github.com/ava-labs/avalanchego/vms/evm/imp/metrics"
	"github.com/ava-labs/avalanchego/vms/evm/network"
	"github.com/ava-labs/avalanchego/vms/evm/sae"
	"github.com/ava-labs/avalanchego/vms/evm/statesync"
	"github.com/ava-labs/avalanchego/vms/evm/types"

	apimetrics "github.com/ava-labs/avalanchego/api/metrics"
	avadb "github.com/ava-labs/avalanchego/database"
	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	l1api "github.com/ava-labs/avalanchego/vms/evm/imp/l1s/api"
	l1warp "github.com/ava-labs/avalanchego/vms/evm/imp/l1s/warp"
	saewarp "github.com/ava-labs/avalanchego/vms/evm/imp/warp"
)

var _ adaptor.ChainVM[*blocks.Block] = (*VM)(nil)

// VM wraps an [sae.VM] with the pieces specific to L1s.
type VM struct {
	*sae.VM           // created by [VM.SetState] as bootstrapping or normal op
	*network.Network  // created by [VM.Initialize]
	*stateSyncHandler // created by [VM.Initialize]

	// now is the clock provided to the [sae.VM], the uptime tracker, and the
	// block-building pacer.
	now   func() time.Time
	pacer *blockdelay.Pacer // created by [VM.SetState] as bootstrapping or normal op

	uptime *uptimetracker.UptimeTracker
	// startUptimeSync starts periodically syncing [VM.uptime] with the
	// validator set. It MUST be called at most once, guaranteed using
	// [VM.startUptimeSyncOnce].
	startUptimeSync     func(context.Context) error
	startUptimeSyncOnce sync.Once

	mode utils.Atomic[snow.State]
	// finishInitialize creates [VM.VM] and everything that depends on it. It
	// MUST be called at most once, guaranteed using [VM.finishInitializeOnce].
	finishInitialize     func(context.Context) error
	finishInitializeOnce sync.Once
	handlers             *api.MutableHTTPHandlers

	closeMu sync.Mutex
	onClose []func(context.Context) error // called in reverse order on shutdown
	closed  bool
}

var errAlreadyClosed = errors.New("already closed")

// Initialize initializes the VM.
func (vm *VM) Initialize(
	ctx context.Context,
	snowCtx *snow.Context,
	avaDB avadb.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	_ []*snowcommon.Fx,
	appSender snowcommon.AppSender,
) (retErr error) {
	// Release every resource of a partially-failed initialization. This MUST
	// be registered before closeMu is acquired so that the deferred unlock
	// runs before the rollback's Shutdown re-acquires the lock.
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

	userConfig, err := parseConfig(snowCtx, configBytes)
	if err != nil {
		return fmt.Errorf("parsing user config: %w", err)
	}
	snowCtx.Log.Info("initializing L1 VM",
		zap.Reflect("config", userConfig),
	)

	if userConfig.useStandaloneDatabase(snowCtx.Log, avaDB) {
		db, err := userConfig.standaloneDatabase(snowCtx)
		if err != nil {
			return fmt.Errorf("opening standalone database: %w", err)
		}
		vm.onClose = append(vm.onClose, func(context.Context) error {
			return db.Close()
		})
		avaDB = db
	}

	genesis, err := parseGenesis(snowCtx, genesisBytes, upgradeBytes)
	if err != nil {
		return fmt.Errorf("parsing genesis: %w", err)
	}

	ethDB := types.NewChainEthDB(avaDB)
	if err := genesis.verifyAndWriteBlock(ethDB); err != nil {
		return fmt.Errorf("writing genesis block: %w", err)
	}

	warpMessages, err := userConfig.WarpMessages()
	if err != nil {
		return err
	}

	reg, err := apimetrics.MakeAndRegister(snowCtx.Metrics, "l1")
	if err != nil {
		return fmt.Errorf("making metrics: %w", err)
	}
	chainMetrics, err := metrics.New(reg)
	if err != nil {
		return fmt.Errorf("registering L1 VM metrics: %w", err)
	}

	warpStorage := saewarp.NewStorage(avaDB, warpMessages...)
	saeConfig := userConfig.saeConfig(vm.now)
	hooks := hookadaptor.Convert(newHooks(
		snowCtx,
		genesis.Config,
		saeConfig.Now,
		userConfig.Desired(),
		warpStorage,
		userConfig.FeeRecipient,
		chainMetrics,
	))

	vm.Network, err = network.New(snowCtx, appSender, userConfig.NetworkOptions()...)
	if err != nil {
		return fmt.Errorf("creating network: %w", err)
	}

	if err := snowCtx.Metrics.Register(firewood.Scheme, ffi.Gatherer{}); err != nil {
		return fmt.Errorf("registering firewood metrics: %w", err)
	}

	vm.stateSyncHandler, err = newStateSyncHandler(
		userConfig.sae().StateSyncConfig(snowCtx.NetworkID),
		ethDB,
		snowCtx,
		vm.Network,
		hooks,
	)
	if err != nil {
		return fmt.Errorf("creating state sync handler: %w", err)
	}
	vm.onClose = append(vm.onClose, vm.stateSyncHandler.Shutdown)

	vm.uptime, err = uptimetracker.New(snowCtx.ValidatorState, snowCtx.SubnetID, prefixdb.New(validatorsDBPrefix, avaDB), unixClock(vm.now))
	if err != nil {
		return fmt.Errorf("creating uptime tracker: %w", err)
	}
	vm.onClose = append(vm.onClose, func(context.Context) error {
		return vm.uptime.Shutdown()
	})
	vm.startUptimeSync = func(ctx context.Context) error {
		vm.closeMu.Lock()
		defer vm.closeMu.Unlock()
		if vm.closed {
			return errAlreadyClosed
		}

		// Sync before returning so that uptimes are queryable as soon as the
		// node is in normal operation.
		if err := vm.uptime.Sync(ctx); err != nil {
			return fmt.Errorf("syncing uptime tracker: %w", err)
		}

		syncCtx, cancelSync := context.WithCancel(context.Background())
		var syncWG sync.WaitGroup
		syncWG.Go(func() {
			ticker := time.NewTicker(uptimeSyncPeriod)
			defer ticker.Stop()
			for {
				select {
				case <-syncCtx.Done():
					return
				case <-ticker.C:
					if err := vm.uptime.Sync(syncCtx); err != nil {
						snowCtx.Log.Error("syncing uptime tracker", zap.Error(err))
					}
				}
			}
		})
		vm.onClose = append(vm.onClose, func(context.Context) error {
			cancelSync()
			syncWG.Wait()
			return nil
		})
		return nil
	}

	vm.handlers = api.NewMutableHTTPHandlers(sae.HandlerPaths...)

	// The [sae.VM] MUST NOT be created until any state sync has finished, as it
	// opens the state that the sync writes.
	vm.finishInitialize = func(ctx context.Context) error {
		vm.closeMu.Lock()
		defer vm.closeMu.Unlock()
		if vm.closed {
			return errAlreadyClosed
		}

		tdbConfig := saeConfig.DBConfig.TrieDBConfig(snowCtx.ChainDataDir, snowCtx.Log)
		if err := genesis.setupTrieDB(ethDB, tdbConfig); err != nil {
			return fmt.Errorf("setting up genesis trie: %w", err)
		}

		// Uses of [sae.VM] are NOT protected by [VM.closeMu]. However,
		// [VM.activeHandler] ensures that they only occur after [VM.SetState]
		// has called this function without error.
		var err error
		vm.VM, err = sae.NewVM(ctx, hooks, saeConfig, snowCtx, genesis.Config, ethDB, vm.Network)
		if err != nil {
			return fmt.Errorf("creating SAE VM: %w", err)
		}
		vm.onClose = append(vm.onClose, vm.VM.Shutdown)
		vm.pacer = blockdelay.NewPacer(vm.now, vm.VM)

		warpVerifier := l1warp.NewVerifier(vm.VM, warpStorage, vm.uptime)
		if err := saewarp.RegisterHandler(vm.Network, warpVerifier, snowCtx.WarpSigner); err != nil {
			return fmt.Errorf("registering warp signature handler: %w", err)
		}

		if userConfig.APIs.Contains(validatorAPI) {
			if err := vm.VM.RPCServer().RegisterName(validatorNamespace, l1api.NewValidator(vm.uptime)); err != nil {
				return fmt.Errorf("registering %q RPC namespace: %w", validatorNamespace, err)
			}
		}
		handlers, err := vm.VM.CreateHandlers(ctx)
		if err != nil {
			return fmt.Errorf("creating SAE handlers: %w", err)
		}
		vm.handlers.Set(handlers)

		tdb, snaps := vm.VM.EVMState()
		// Firewood can only be opened once. HashDB is reopened so that serving
		// peers doesn't pollute the execution cache.
		if saeConfig.DBConfig.Scheme != firewood.Scheme {
			tdb = triedb.NewDatabase(ethDB, tdbConfig)
			vm.onClose = append(vm.onClose, func(context.Context) error {
				return tdb.Close()
			})
		}
		if err := statesync.RegisterHandlers(snowCtx.Log, vm.Network.Network, ethDB, tdb, snaps, reg); err != nil {
			return fmt.Errorf("registering state sync server: %w", err)
		}
		return nil
	}

	snowCtx.Log.Info("initialized L1 VM")
	return nil
}

// unixClock is a time source for [uptimetracker.New].
type unixClock func() time.Time

func (c unixClock) UnixTime() time.Time { return c().Truncate(time.Second) }

const (
	// validatorNamespace is the JSON-RPC namespace of [l1api.Validator].
	validatorNamespace = "validator"
	// uptimeSyncPeriod is the period between syncs of [VM.uptime] with the
	// validator set.
	uptimeSyncPeriod = time.Minute
)

// validatorsDBPrefix scopes the uptime tracker's state within the VM database.
var validatorsDBPrefix = []byte("validators")

// Connected forwards to the embedded [network.Network] AFTER notifying the
// uptime tracker.
func (vm *VM) Connected(ctx context.Context, nodeID ids.NodeID, ver *version.Application) error {
	if err := vm.uptime.Connect(nodeID); err != nil {
		return err
	}
	return vm.Network.Connected(ctx, nodeID, ver)
}

// Disconnected forwards to the embedded [network.Network] AFTER notifying the
// uptime tracker.
func (vm *VM) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	if err := vm.uptime.Disconnect(nodeID); err != nil {
		return err
	}
	return vm.Network.Disconnected(ctx, nodeID)
}

// CreateHandlers returns the HTTP handlers of the [sae.VM]. They are
// unusable until the VM is bootstrapping.
func (vm *VM) CreateHandlers(context.Context) (map[string]http.Handler, error) {
	return vm.handlers.AsInterface(), nil
}

// SetState sets the state of the VM. On the first transition to
// [snow.Bootstrapping] or later, it creates the [sae.VM], failing if a state
// sync failed. On the first transition to [snow.NormalOp], it starts syncing
// the uptime tracker with the validator set. Any error returned is fatal.
func (vm *VM) SetState(ctx context.Context, state snow.State) error {
	if state >= snow.Bootstrapping {
		var err error
		vm.finishInitializeOnce.Do(func() {
			if err = vm.stateSyncHandler.SyncError(); err != nil {
				err = fmt.Errorf("state sync: %w", err)
				return
			}
			err = vm.finishInitialize(ctx)
		})
		if err != nil {
			return err
		}
		if err := vm.VM.SetState(ctx, state); err != nil {
			return fmt.Errorf("setting SAE VM state: %w", err)
		}
	}

	// MUST happen after [VM.VM] is set, which [VM.activeHandler] relies on.
	vm.mode.Set(state)
	if state != snow.NormalOp {
		return nil
	}
	var err error
	vm.startUptimeSyncOnce.Do(func() {
		err = vm.startUptimeSync(ctx)
	})
	return err
}

var (
	_ stateDependent = (*sae.VM)(nil)
	_ stateDependent = (*stateSyncHandler)(nil)
)

// stateDependent methods are served by the [stateSyncHandler] until the
// [sae.VM] exists.
type stateDependent interface {
	HealthCheck(context.Context) (any, error)
	ParseBlock(context.Context, []byte) (*blocks.Block, error)
	GetBlock(context.Context, ids.ID) (*blocks.Block, error)
	GetBlockIDAtHeight(context.Context, uint64) (ids.ID, error)
	LastAccepted(context.Context) (ids.ID, error)
}

func (vm *VM) activeHandler() stateDependent {
	if vm.mode.Get() >= snow.Bootstrapping {
		return vm.VM
	}
	return vm.stateSyncHandler
}

// HealthCheck returns the current health status of the VM.
func (vm *VM) HealthCheck(ctx context.Context) (any, error) {
	return vm.activeHandler().HealthCheck(ctx)
}

// ParseBlock parses a block from bytes.
func (vm *VM) ParseBlock(ctx context.Context, blockBytes []byte) (*blocks.Block, error) {
	return vm.activeHandler().ParseBlock(ctx, blockBytes)
}

// GetBlock returns the [blocks.Block] with the given ID.
func (vm *VM) GetBlock(ctx context.Context, id ids.ID) (*blocks.Block, error) {
	return vm.activeHandler().GetBlock(ctx, id)
}

// GetBlockIDAtHeight returns the ID of the accepted block at the given height.
func (vm *VM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	return vm.activeHandler().GetBlockIDAtHeight(ctx, height)
}

// LastAccepted returns the ID of the last accepted block.
func (vm *VM) LastAccepted(ctx context.Context) (ids.ID, error) {
	return vm.activeHandler().LastAccepted(ctx)
}

// WaitForEvent waits for the next event of the current state. While state
// syncing, that is the end of the sync. Otherwise, it waits until the ACP-226
// minimum block delay since the preferred block has elapsed, then waits for
// the SAE VM to produce an event.
func (vm *VM) WaitForEvent(ctx context.Context) (snowcommon.Message, error) {
	switch vm.mode.Get() {
	case snow.Initializing:
		// No event can occur while initializing.
		<-ctx.Done()
		return 0, context.Cause(ctx)
	case snow.StateSyncing:
		return vm.stateSyncHandler.WaitForEvent(ctx)
	}

	return vm.pacer.WaitForEvent(ctx, earliestBlockTime(vm.VM.GetPreference().Header()))
}

// Shutdown releases every resource allocated by [VM.Initialize] in reverse
// order. It is idempotent and safe to call after a partially-failed
// [VM.Initialize].
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
