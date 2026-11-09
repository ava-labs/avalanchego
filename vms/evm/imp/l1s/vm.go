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

	"go.uber.org/zap"

	// Force-load precompiles to trigger registration.
	_ "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/registry"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/evm/adaptor"
	"github.com/ava-labs/avalanchego/vms/evm/blocks"
	"github.com/ava-labs/avalanchego/vms/evm/imp/l1s/hookadaptor"
	"github.com/ava-labs/avalanchego/vms/evm/imp/l1s/validators"
	"github.com/ava-labs/avalanchego/vms/evm/network"
	"github.com/ava-labs/avalanchego/vms/evm/sae"
	"github.com/ava-labs/avalanchego/vms/evm/types"

	apimetrics "github.com/ava-labs/avalanchego/api/metrics"
	avadb "github.com/ava-labs/avalanchego/database"
	legacylog "github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/log"
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

	ctx *snow.Context

	// now is the clock provided to the [sae.VM] and is used for block building.
	now func() time.Time
	// clock drives the validator uptime tracker. It is allocated by
	// [VM.Initialize] if nil.
	clock *mockable.Clock

	uptime *validators.Uptime

	// handlers is built once by [VM.Initialize] and served verbatim by
	// [VM.CreateHandlers].
	handlers map[string]http.Handler

	lastWaitForEvent utils.Atomic[time.Time]

	closeMu sync.Mutex
	onClose []func(context.Context) error // called in reverse order on shutdown
	closed  bool
}

// New constructs a new [VM] that uses the wall clock.
func New() *VM {
	return &VM{
		now: time.Now,
	}
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

	userConfig, err := parseConfig(configBytes)
	if err != nil {
		return fmt.Errorf("parsing user config: %w", err)
	}
	if err := applyLogLevel(userConfig.LogLevel, snowCtx); err != nil {
		return err
	}
	snowCtx.Log.Info("initializing L1 VM",
		zap.Reflect("config", userConfig),
	)

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

	warpMessages, err := saewarp.ParseOffChainMessages(userConfig.WarpOffChainMessages)
	if err != nil {
		return err
	}

	reg, err := apimetrics.MakeAndRegister(snowCtx.Metrics, "l1")
	if err != nil {
		return fmt.Errorf("making metrics: %w", err)
	}
	chainMetrics, err := sae.NewMinBlockDelayMetric(reg)
	if err != nil {
		return fmt.Errorf("registering L1 VM metrics: %w", err)
	}

	warpStorage := saewarp.NewStorage(avaDB, warpMessages...)
	hooks := newHooks(
		snowCtx,
		genesis.Config,
		saeConfig.Now,
		userConfig.desired(),
		warpStorage,
		userConfig.feeRecipient(genesis.Config, snowCtx.Log),
		chainMetrics,
	)

	vm.Network, err = network.New(snowCtx, appSender)
	if err != nil {
		return fmt.Errorf("creating network: %w", err)
	}

	// TODO: Support state sync, which requires the [sae.VM] to be created
	// after the node has finished state syncing.
	vm.VM, err = sae.NewVM(ctx, hookadaptor.Convert(hooks), saeConfig, snowCtx, genesis.Config, ethDB, vm.Network)
	if err != nil {
		return fmt.Errorf("creating SAE VM: %w", err)
	}
	vm.ctx = snowCtx
	vm.onClose = append(vm.onClose, vm.VM.Shutdown)

	if vm.clock == nil {
		vm.clock = &mockable.Clock{}
	}
	vm.uptime, err = validators.New(snowCtx.ValidatorState, snowCtx.SubnetID, avaDB, vm.clock, snowCtx.Log)
	if err != nil {
		return fmt.Errorf("creating uptime tracker: %w", err)
	}
	vm.onClose = append(vm.onClose, func(context.Context) error {
		return vm.uptime.Shutdown()
	})

	warpVerifier := l1warp.NewVerifier(vm.VM, warpStorage, vm.uptime)
	if err := saewarp.RegisterHandler(vm.Network, warpVerifier, snowCtx.WarpSigner); err != nil {
		return fmt.Errorf("registering warp signature handler: %w", err)
	}

	if vm.handlers, err = vm.newHandlers(ctx); err != nil {
		return fmt.Errorf("creating HTTP handlers: %w", err)
	}

	snowCtx.Log.Info("initialized L1 VM")
	return nil
}

// applyLogLevel applies the operator's `log-level` (when set) to both the
// process-global libevm logger and the chain's avalanchego logger; see
// [config.LogLevel] for the exact semantics.
func applyLogLevel(logLevel string, snowCtx *snow.Context) error {
	if logLevel == "" {
		return nil
	}
	alias, aliasErr := snowCtx.BCLookup.PrimaryAlias(snowCtx.ChainID)
	if aliasErr != nil {
		alias = snowCtx.ChainID.String()
	}
	// TODO(ceyonur): Add JSON format support
	if _, err := legacylog.InitLogger(alias, logLevel, false, snowCtx.Log); err != nil {
		return fmt.Errorf("initializing libevm logger: %w", err)
	}
	if avaLevel, levelErr := logging.ToLevel(logLevel); levelErr == nil {
		snowCtx.Log.SetLevel(avaLevel)
	} else {
		snowCtx.Log.Warn("could not map config log-level to avalanchego level; SAE-side logger left at avalanchego-configured level",
			zap.String("logLevel", logLevel),
			zap.Error(levelErr),
		)
	}
	return nil
}

const (
	validatorsServiceName       = "validators"
	validatorsHTTPExtensionPath = "/" + validatorsServiceName
)

// newHandlers returns the HTTP handlers exposed by the underlying SAE VM
// augmented with the L1 VM's legacy-compatible `eth_*` methods and validators
// service.
func (vm *VM) newHandlers(ctx context.Context) (map[string]http.Handler, error) {
	ethExtras := l1api.NewEthExtrasAPI(vm.VM.GethRPCBackends())
	if err := vm.VM.RPCServer().RegisterName("eth", ethExtras); err != nil {
		return nil, fmt.Errorf("RPCServer.RegisterName(\"eth\", *EthExtrasAPI): %w", err)
	}

	m, err := vm.VM.CreateHandlers(ctx)
	if err != nil {
		return nil, err
	}

	service := l1api.NewValidatorsAPI(vm.ctx.ValidatorState, vm.ctx.SubnetID, vm.uptime, vm.Network.ValidatorPeers)
	handler, err := rpc.NewHandler(validatorsServiceName, service)
	if err != nil {
		return nil, fmt.Errorf("rpc.NewHandler(%s, ...): %w", validatorsServiceName, err)
	}
	m[validatorsHTTPExtensionPath] = handler
	return m, nil
}

// CreateHandlers returns the HTTP handlers built by [VM.Initialize].
func (vm *VM) CreateHandlers(context.Context) (map[string]http.Handler, error) {
	return vm.handlers, nil
}

// Connected forwards to the embedded [network.Network] AFTER notifying the
// validators manager.
func (vm *VM) Connected(ctx context.Context, nodeID ids.NodeID, ver *version.Application) error {
	if err := vm.uptime.Connect(nodeID); err != nil {
		return err
	}
	return vm.Network.Connected(ctx, nodeID, ver)
}

// Disconnected forwards to the embedded [network.Network] AFTER notifying the
// validators manager.
func (vm *VM) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	if err := vm.uptime.Disconnect(nodeID); err != nil {
		return err
	}
	return vm.Network.Disconnected(ctx, nodeID)
}

// SetState forwards to `*sae.VM.SetState` and, on the first transition
// to `snow.NormalOp`, hands off to the validators manager (which
// performs the initial uptime sync and spawns the periodic-sync
// goroutine; both are no-ops on subsequent calls).
func (vm *VM) SetState(ctx context.Context, state snow.State) error {
	if err := vm.VM.SetState(ctx, state); err != nil {
		return err
	}
	if state != snow.NormalOp {
		return nil
	}
	return vm.uptime.Dispatch()
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
