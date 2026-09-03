// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package l1 implements the SAE-based EVM for Avalanche L1s atop [sae.VM]. It
// composes the L1 block-building hooks (precompile and state upgrades,
// allowlist admission, reward routing, and the header-encoded ACP-224 runtime
// gas config), the shared SAE warp service extended with validator-uptime
// attestations, and the legacy-compatible JSON-RPC surface.
package l1

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

	// Force-load precompiles to trigger registration.
	_ "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/registry"

	"github.com/ava-labs/avalanchego/graft/evm/utils/rpc"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/l1/validators"
	"github.com/ava-labs/avalanchego/vms/saevm/network"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"

	apimetrics "github.com/ava-labs/avalanchego/api/metrics"
	avadb "github.com/ava-labs/avalanchego/database"
	legacylog "github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/log"
	l1api "github.com/ava-labs/avalanchego/vms/saevm/l1/api"
	l1warp "github.com/ava-labs/avalanchego/vms/saevm/l1/warp"
	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
	saewarp "github.com/ava-labs/avalanchego/vms/saevm/warp"
)

var _ adaptor.ChainVM[*blocks.Block] = (*VM)(nil)

// VM is a harness around an [sae.VM].
type VM struct {
	*sae.VM          // created by [VM.Initialize]
	*network.Network // created by [VM.Initialize]

	ctx *snow.Context

	clock mockable.Clock

	uptime *validators.Uptime

	// handlers is built once by [VM.Initialize] and served verbatim by
	// [VM.CreateHandlers].
	handlers map[string]http.Handler

	lastWaitForEvent utils.Atomic[time.Time]

	closeMu sync.Mutex
	onClose []func(context.Context) error // called in reverse order on shutdown
	closed  bool
}

// New constructs a new [VM].
func New() *VM {
	return &VM{}
}

var errAlreadyClosed = errors.New("already closed")

// Initialize initializes the VM.
func (v *VM) Initialize(
	ctx context.Context,
	snowCtx *snow.Context,
	avaDB avadb.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	_ []*common.Fx,
	appSender common.AppSender,
) (retErr error) {
	// Release every resource of a partially-failed initialization. This MUST
	// be registered before closeMu is acquired so that the deferred unlock
	// runs before the rollback's Shutdown re-acquires the lock.
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, v.Shutdown(ctx))
		}
	}()

	v.closeMu.Lock() // [VM.Shutdown] acquires this lock.
	defer v.closeMu.Unlock()
	if v.closed {
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

	saeConfig := userConfig.saeConfig(v.clock.Time)

	db := saetypes.NewChainEthDB(avaDB)
	tdb := triedb.NewDatabase(db, saeConfig.DBConfig.TrieDBConfig(snowCtx.ChainDataDir, snowCtx.Log))

	genesis, err := parseGenesis(snowCtx, genesisBytes, upgradeBytes)
	if err != nil {
		return fmt.Errorf("parsing genesis: %w", err)
	}

	genesisHash := genesis.ToBlock().Hash()
	lastAcceptedHash := readLastAcceptedHash(db, genesisHash)
	snowCtx.Log.Info("setting up the genesis",
		zap.Stringer("lastAcceptedID", ids.ID(lastAcceptedHash)),
	)

	config, _, err := core.SetupGenesisBlock(db, tdb, genesis, lastAcceptedHash, false /*skipChainConfigCheckCompatible*/)
	if err != nil {
		return fmt.Errorf("core.SetupGenesisBlock(...): %w", err)
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
		config,
		saeConfig.Now,
		userConfig.desired(),
		warpStorage,
		userConfig.feeRecipient(config, snowCtx.Log),
		chainMetrics,
	)

	v.Network, err = network.New(snowCtx, appSender)
	if err != nil {
		return fmt.Errorf("creating network: %w", err)
	}

	inner, err := sae.NewVM(ctx, hooks, saeConfig, snowCtx, config, db, v.Network)
	if err != nil {
		return fmt.Errorf("creating SAE VM: %w", err)
	}
	v.VM = inner
	v.ctx = snowCtx
	v.onClose = append(v.onClose, v.VM.Shutdown)

	v.uptime, err = validators.New(snowCtx.ValidatorState, snowCtx.SubnetID, avaDB, &v.clock, snowCtx.Log)
	if err != nil {
		return fmt.Errorf("creating uptime tracker: %w", err)
	}
	v.onClose = append(v.onClose, func(context.Context) error {
		return v.uptime.Shutdown()
	})

	warpVerifier := l1warp.NewVerifier(inner, warpStorage, v.uptime)
	if err := saewarp.RegisterHandler(v.Network, warpVerifier, snowCtx.WarpSigner); err != nil {
		return fmt.Errorf("registering warp signature handler: %w", err)
	}

	if v.handlers, err = v.newHandlers(ctx); err != nil {
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
func (v *VM) newHandlers(ctx context.Context) (map[string]http.Handler, error) {
	ethExtras := l1api.NewEthExtrasAPI(v.VM.GethRPCBackends())
	if err := v.VM.RPCServer().RegisterName("eth", ethExtras); err != nil {
		return nil, fmt.Errorf("RPCServer.RegisterName(\"eth\", *EthExtrasAPI): %w", err)
	}

	m, err := v.VM.CreateHandlers(ctx)
	if err != nil {
		return nil, err
	}

	service := l1api.NewValidatorsAPI(v.ctx.ValidatorState, v.ctx.SubnetID, v.uptime, v.Network.ValidatorPeers)
	handler, err := rpc.NewHandler(validatorsServiceName, service)
	if err != nil {
		return nil, fmt.Errorf("rpc.NewHandler(%s, ...): %w", validatorsServiceName, err)
	}
	m[validatorsHTTPExtensionPath] = handler
	return m, nil
}

// CreateHandlers returns the HTTP handlers built by [VM.Initialize].
func (v *VM) CreateHandlers(context.Context) (map[string]http.Handler, error) {
	return v.handlers, nil
}

// Connected forwards to the embedded [network.Network] AFTER notifying the
// validators manager.
func (v *VM) Connected(ctx context.Context, nodeID ids.NodeID, ver *version.Application) error {
	if err := v.uptime.Connect(nodeID); err != nil {
		return err
	}
	return v.Network.Connected(ctx, nodeID, ver)
}

// Disconnected forwards to the embedded [network.Network] AFTER notifying the
// validators manager.
func (v *VM) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	if err := v.uptime.Disconnect(nodeID); err != nil {
		return err
	}
	return v.Network.Disconnected(ctx, nodeID)
}

// SetState forwards to `*sae.VM.SetState` and, on the first transition
// to `snow.NormalOp`, hands off to the validators manager (which
// performs the initial uptime sync and spawns the periodic-sync
// goroutine; both are no-ops on subsequent calls).
func (v *VM) SetState(ctx context.Context, state snow.State) error {
	if err := v.VM.SetState(ctx, state); err != nil {
		return err
	}
	if state != snow.NormalOp {
		return nil
	}
	return v.uptime.Dispatch()
}

// minWaitForEventDelay is the minimum spacing between consecutive
// [VM.WaitForEvent] returns, preventing busy looping when the chain is more
// advanced than the mempool.
const minWaitForEventDelay = 100 * time.Millisecond

// WaitForEvent waits until both the ACP-226 minimum block delay since the
// preferred block and the busy-loop throttle have elapsed, then waits for
// the SAE VM to produce an event.
func (v *VM) WaitForEvent(ctx context.Context) (common.Message, error) {
	until := v.lastWaitForEvent.Get().Add(minWaitForEventDelay)
	if buildTime := earliestBlockTime(v.VM.GetPreference().Header()); buildTime.After(until) {
		until = buildTime
	}
	if err := v.waitUntil(ctx, until); err != nil {
		return 0, err
	}

	msg, err := v.VM.WaitForEvent(ctx)
	if err == nil {
		v.lastWaitForEvent.Set(v.clock.Time())
	}
	return msg, err
}

// waitUntil blocks until the VM clock reaches t, returning early with the
// cancellation cause if ctx is canceled first.
func (v *VM) waitUntil(ctx context.Context, t time.Time) error {
	timeToWait := t.Sub(v.clock.Time())
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
func (v *VM) Shutdown(ctx context.Context) error {
	v.closeMu.Lock()
	defer v.closeMu.Unlock()
	if v.closed {
		return nil
	}
	v.closed = true

	errs := make([]error, len(v.onClose))
	for i, f := range slices.Backward(v.onClose) {
		errs[i] = f(ctx)
	}
	v.onClose = nil
	return errors.Join(errs...)
}
