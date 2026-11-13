// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package l1s implements the Subnet-EVM compatible L1 VM atop [sae.VM].
package l1s

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"go.uber.org/zap"

	// Force-load precompiles to trigger registration.
	_ "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/registry"

	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/evm/uptimetracker"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/evm/adaptor"
	"github.com/ava-labs/avalanchego/vms/evm/blocks"
	"github.com/ava-labs/avalanchego/vms/evm/imp/l1s/hookadaptor"
	"github.com/ava-labs/avalanchego/vms/evm/imp/metrics"
	"github.com/ava-labs/avalanchego/vms/evm/network"
	"github.com/ava-labs/avalanchego/vms/evm/sae"
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
	*sae.VM          // created by [VM.Initialize]
	*network.Network // created by [VM.Initialize]

	// now is the clock provided to the [sae.VM] and is used for block building.
	now func() time.Time
	// clock drives the validator uptime tracker. It is allocated by
	// [VM.Initialize] if nil.
	clock *mockable.Clock

	uptime *uptimetracker.UptimeTracker
	// startUptimeSync starts periodically syncing [VM.uptime] with the
	// validator set. It MUST be called at most once, guaranteed using
	// [VM.startUptimeSyncOnce].
	startUptimeSync     func(context.Context) error
	startUptimeSyncOnce sync.Once

	lastWaitForEvent utils.Atomic[time.Time]

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

	saeConfig := userConfig.saeConfig(vm.now)
	tdbConfig := saeConfig.DBConfig.TrieDBConfig(snowCtx.ChainDataDir, snowCtx.Log)
	if err := genesis.setupTrieDB(ethDB, tdbConfig); err != nil {
		return fmt.Errorf("setting up genesis trie: %w", err)
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
	hooks := newHooks(
		snowCtx,
		genesis.Config,
		saeConfig.Now,
		userConfig.Desired(),
		warpStorage,
		userConfig.FeeRecipient,
		chainMetrics,
	)

	vm.Network, err = network.New(snowCtx, appSender, userConfig.NetworkOptions()...)
	if err != nil {
		return fmt.Errorf("creating network: %w", err)
	}

	// TODO: Support state sync, which requires the [sae.VM] to be created
	// after the node has finished state syncing.
	vm.VM, err = sae.NewVM(ctx, hookadaptor.Convert(hooks), saeConfig, snowCtx, genesis.Config, ethDB, vm.Network)
	if err != nil {
		return fmt.Errorf("creating SAE VM: %w", err)
	}
	vm.onClose = append(vm.onClose, vm.VM.Shutdown)

	if vm.clock == nil {
		vm.clock = &mockable.Clock{}
	}
	vm.uptime, err = uptimetracker.New(snowCtx.ValidatorState, snowCtx.SubnetID, prefixdb.New(validatorsDBPrefix, avaDB), vm.clock)
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

	warpVerifier := l1warp.NewVerifier(vm.VM, warpStorage, vm.uptime)
	if err := saewarp.RegisterHandler(vm.Network, warpVerifier, snowCtx.WarpSigner); err != nil {
		return fmt.Errorf("registering warp signature handler: %w", err)
	}

	if userConfig.APIs.Contains(validatorAPI) {
		if err := vm.VM.RPCServer().RegisterName(validatorNamespace, l1api.NewValidator(vm.uptime)); err != nil {
			return fmt.Errorf("registering %q RPC namespace: %w", validatorNamespace, err)
		}
	}

	snowCtx.Log.Info("initialized L1 VM")
	return nil
}

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

// SetState forwards to [sae.VM.SetState] and, on the first transition to
// [snow.NormalOp], starts syncing the uptime tracker with the validator set.
func (vm *VM) SetState(ctx context.Context, state snow.State) error {
	if err := vm.VM.SetState(ctx, state); err != nil {
		return err
	}
	if state != snow.NormalOp {
		return nil
	}
	var err error
	vm.startUptimeSyncOnce.Do(func() {
		err = vm.startUptimeSync(ctx)
	})
	return err
}

// minWaitForEventDelay is the minimum spacing between consecutive
// [VM.WaitForEvent] returns, preventing busy looping when the chain is more
// advanced than the mempool.
const minWaitForEventDelay = 100 * time.Millisecond

// WaitForEvent waits until both the ACP-226 minimum block delay since the
// preferred block and the busy-loop throttle have elapsed, then waits for
// the SAE VM to produce an event.
func (vm *VM) WaitForEvent(ctx context.Context) (snowcommon.Message, error) {
	until := vm.lastWaitForEvent.Get().Add(minWaitForEventDelay)
	if buildTime := earliestBlockTime(vm.VM.GetPreference().Header()); buildTime.After(until) {
		until = buildTime
	}
	if err := vm.waitUntil(ctx, until); err != nil {
		return 0, err
	}

	msg, err := vm.VM.WaitForEvent(ctx)
	if err == nil {
		vm.lastWaitForEvent.Set(vm.now())
	}
	return msg, err
}

// waitUntil blocks until the VM clock reaches t, returning early with the
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
	}
	return nil
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
