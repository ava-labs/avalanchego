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
	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"github.com/ava-labs/libevm/triedb"

	// TODO(alarso16): Remove the dependency on the global module registration.
	_ "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/registry" // register precompiles

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/evm/adaptor"
	"github.com/ava-labs/avalanchego/vms/evm/blocks"
	"github.com/ava-labs/avalanchego/vms/evm/firewood"
	"github.com/ava-labs/avalanchego/vms/evm/imp/l1s/hookadaptor"
	"github.com/ava-labs/avalanchego/vms/evm/network"
	"github.com/ava-labs/avalanchego/vms/evm/sae"
	"github.com/ava-labs/avalanchego/vms/evm/sae/rpc"
	"github.com/ava-labs/avalanchego/vms/evm/saedb"
	"github.com/ava-labs/avalanchego/vms/evm/statesync"
	"github.com/ava-labs/avalanchego/vms/evm/statesync/acceptor"
	"github.com/ava-labs/avalanchego/vms/evm/types"

	apimetrics "github.com/ava-labs/avalanchego/api/metrics"
	avadb "github.com/ava-labs/avalanchego/database"
	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

var (
	_ adaptor.ChainVM[*blocks.Block]         = (*VM)(nil)
	_ adaptor.SyncableVM[*statesync.Summary] = (*VM)(nil)
)

// VM wraps an [sae.VM] with the pieces specific to L1s.
type VM struct {
	*sae.VM                                     // created by [VM.SetState] as bootstrapping or normal op
	*network.Network                            // created by [VM.Initialize]
	*statesync.Handler                          // created by [VM.Initialize]
	*acceptor.AsyncAcceptor[*statesync.Summary] // created by [VM.Initialize]

	// now is the clock provided to the [sae.VM] and is used for block building.
	now func() time.Time
	// commitInterval overrides [saedb.DefaultCommitInterval] if non-zero.
	//
	// TODO: Replace with the user config.
	commitInterval uint64

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
	upgradeBytes []byte,
	_ []byte, // TODO: Parse the user config.
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

	genesis, err := parseGenesis(snowCtx, genesisBytes, upgradeBytes)
	if err != nil {
		return fmt.Errorf("parsing genesis: %w", err)
	}

	reg, err := apimetrics.MakeAndRegister(snowCtx.Metrics, "l1s")
	if err != nil {
		return fmt.Errorf("making metrics: %w", err)
	}

	// When run as a plugin, the rpcchainvm wraps the provided database, so
	// [prefixdb.New] and [prefixdb.NewNested] are equivalent. Using
	// [prefixdb.NewNested] keeps the database format unchanged if this VM is
	// ever run in-process, where [prefixdb.New] would compress the prefixes.
	ethDB := types.NewEthDB(prefixdb.NewNested(ethDBPrefix, avaDB))
	if err := genesis.verifyAndWriteBlock(ethDB); err != nil {
		return fmt.Errorf("writing genesis block: %w", err)
	}

	// TODO: Allow the mempool to be configured by the user config.
	mempoolConfig := legacypool.DefaultConfig
	mempoolConfig.NoLocals = true
	saeConfig := sae.Config{
		MempoolConfig: mempoolConfig,
		DBConfig: saedb.Config{
			TrieCacheMiB:     saedb.DefaultTrieCacheSizeMiB,
			SnapshotCacheMiB: saedb.DefaultSnapshotCacheSizeMiB,
			CommitInterval:   saedb.DefaultCommitInterval,
		},
		RPCConfig: rpc.Config{
			APIs: rpc.DefaultAPIs(),
		},
		Now: vm.now,
	}
	if vm.commitInterval != 0 {
		saeConfig.DBConfig.CommitInterval = vm.commitInterval
	}

	vm.Network, err = network.New(snowCtx, appSender)
	if err != nil {
		return fmt.Errorf("creating network: %w", err)
	}

	if err := snowCtx.Metrics.Register(firewood.Scheme, ffi.Gatherer{}); err != nil {
		return fmt.Errorf("registering firewood metrics: %w", err)
	}

	// TODO: Support state sync, which requires the [sae.VM] to be created
	// after the node has finished state syncing.
	hooks := hookadaptor.Convert(newHooks(snowCtx, genesis.Config, vm.now))
	// TODO: Allow state sync to be configured by the user config.
	syncConfig := statesync.Config{
		DBConfig: saeConfig.DBConfig,
		Enabled:  true,
	}
	vm.Handler, err = statesync.New(syncConfig, ethDB, snowCtx, vm.Network, hooks)
	if err != nil {
		return fmt.Errorf("creating statesync handler: %w", err)
	}
	vm.AsyncAcceptor = acceptor.New(syncConfig.Enabled, func() (acceptor.Syncer[*statesync.Summary], error) {
		return statesync.NewSyncer(syncConfig, hooks, snowCtx, vm.Network, ethDB), nil
	})
	vm.onClose = append(vm.onClose, vm.AsyncAcceptor.Shutdown)

	vm.handlers = api.NewMutableHTTPHandlers(sae.HandlerPaths...)

	// [VM.finishInitialize] adds the [sae.VM] after all necessary state is available.
	// This MUST be called exactly once, guaranteed using [VM.finishInitializeOnce].
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
		// [VM.activeHandler] ensures that methods accessing [sae.VM] only occur
		// AFTER [vm.SetState] is called with [snow.Bootstrapping] or
		// [snow.NormalOp], which guarantees that this method has returned no error.
		var err error
		vm.VM, err = sae.NewVM(ctx, hooks, saeConfig, snowCtx, genesis.Config, ethDB, vm.Network)
		if err != nil {
			return fmt.Errorf("creating SAE VM: %w", err)
		}
		vm.onClose = append(vm.onClose, vm.VM.Shutdown)

		m, err := vm.VM.CreateHandlers(ctx)
		if err != nil {
			return fmt.Errorf("creating SAE handlers: %w", err)
		}
		vm.handlers.Set(m)

		// Register state sync server
		tdb, snaps := vm.VM.EVMState()
		// Firewood can only be opened once, but a HashDB shouldn't share a
		// cache with execution.
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

	return nil
}

// CreateHandlers returns the HTTP handlers exposed by the underlying SAE VM.
// None of the handlers are usable until after the [VM] is set as
// bootstrapping/normal operation.
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
			if err = vm.AsyncAcceptor.SyncError(); err != nil {
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

	// MUST occur after [VM.finishInitialize] to avoid race setting [VM.VM].
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

// HealthCheck returns the current health status of the VM.
func (vm *VM) HealthCheck(ctx context.Context) (any, error) {
	if vm.mode.Get() >= snow.Bootstrapping {
		return vm.VM.HealthCheck(ctx)
	}
	return vm.AsyncAcceptor.HealthCheck(ctx)
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

// WaitForEvent waits for the state sync to complete if the VM is state syncing,
// or for the SAE VM to produce an event once it is bootstrapping.
func (vm *VM) WaitForEvent(ctx context.Context) (snowcommon.Message, error) {
	switch vm.mode.Get() {
	case snow.Initializing:
		// no event can occur while the VM is initializing.
		<-ctx.Done()
		return 0, context.Cause(ctx)
	case snow.StateSyncing:
		return vm.AsyncAcceptor.WaitForEvent(ctx)
	}
	return vm.VM.WaitForEvent(ctx)
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
