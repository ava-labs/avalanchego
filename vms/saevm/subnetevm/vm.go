// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnetevm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/triedb"
	"go.uber.org/zap"

	// Force-load precompiles to trigger registration.
	_ "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/registry"

	"github.com/ava-labs/avalanchego/graft/evm/utils/rpc"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/network"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
	"github.com/ava-labs/avalanchego/vms/saevm/subnetevm/validators"

	avadb "github.com/ava-labs/avalanchego/database"
	subnetevmlog "github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/log"
	saehook "github.com/ava-labs/avalanchego/vms/saevm/hook"
	subnetevmapi "github.com/ava-labs/avalanchego/vms/saevm/subnetevm/api"
	subnetevmwarp "github.com/ava-labs/avalanchego/vms/saevm/subnetevm/warp"
	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
	saewarp "github.com/ava-labs/avalanchego/vms/saevm/warp"
)

var _ adaptor.ChainVM[*blocks.Block] = (*VM)(nil)

// VM is a harness around an [sae.VM], providing an `Initialize`
// method that supports being asynchronous since genesis or after a previously
// accepted synchronous block. See [readLastSync] for the resume path.
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
	snowCtx.Log.Info("initializing subnet-evm",
		zap.Reflect("config", userConfig),
	)

	saeConfig := userConfig.saeConfig(v.clock.Time)

	db := saetypes.NewChainEthDB(avaDB)
	tdb := triedb.NewDatabase(db, saeConfig.DBConfig.TrieDBConfig(snowCtx.ChainDataDir, snowCtx.Log))

	genesis, err := parseGenesis(snowCtx, genesisBytes, upgradeBytes)
	if err != nil {
		return fmt.Errorf("parsing genesis: %w", err)
	}

	lastSync, err := lastSynchronousBlock(avaDB, genesis)
	if err != nil {
		return fmt.Errorf("establishing last synchronous block: %w", err)
	}
	snowCtx.Log.Info("setting up the genesis",
		zap.Stringer("lastID", ids.ID(lastSync.Hash())),
		zap.Uint64("lastHeight", lastSync.NumberU64()),
	)

	config, _, err := core.SetupGenesisBlock(db, tdb, genesis, lastSync.Hash(), false /*skipChainConfigCheckCompatible*/)
	if err != nil {
		return fmt.Errorf("core.SetupGenesisBlock(...): %w", err)
	}

	warpMessages, err := userConfig.WarpMessages()
	if err != nil {
		return err
	}

	warpStorage := saewarp.NewStorage(avaDB, warpMessages...)
	hooks := newHooks(
		snowCtx,
		config,
		saeConfig.Now,
		userConfig.desired(),
		warpStorage,
		userConfig.feeRecipient(config, snowCtx.Log),
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

	warpVerifier := subnetevmwarp.NewVerifier(inner, warpStorage, v.uptime)
	if err := saewarp.RegisterHandler(v.Network, warpVerifier, snowCtx.WarpSigner); err != nil {
		return fmt.Errorf("registering warp signature handler: %w", err)
	}

	if v.handlers, err = v.newHandlers(ctx); err != nil {
		return fmt.Errorf("creating HTTP handlers: %w", err)
	}

	snowCtx.Log.Info("initialized subnet-evm")
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
	if _, err := subnetevmlog.InitLogger(alias, logLevel, false, snowCtx.Log); err != nil {
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
// augmented with the subnet-evm-specific `eth_*` methods and the validators
// service.
func (v *VM) newHandlers(ctx context.Context) (map[string]http.Handler, error) {
	ethExtras := subnetevmapi.NewEthExtrasAPI(v.VM.GethRPCBackends())
	if err := v.VM.RPCServer().RegisterName("eth", ethExtras); err != nil {
		return nil, fmt.Errorf("RPCServer.RegisterName(\"eth\", *EthExtrasAPI): %w", err)
	}

	m, err := v.VM.CreateHandlers(ctx)
	if err != nil {
		return nil, err
	}

	service := subnetevmapi.NewValidatorsAPI(v.ctx.ValidatorState, v.ctx.SubnetID, v.uptime, v.Network.ValidatorPeers)
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

// Prevent busy looping when the chain is more advanced than the mempool.
const waitForEventDelay = 100 * time.Millisecond

// WaitForEvent waits for the next event from the VM.
func (v *VM) WaitForEvent(ctx context.Context) (common.Message, error) {
	// Avoid busy looping if we seem like we are ready to build a block, but are
	// encountering an error.
	{
		defer func() {
			v.lastWaitForEvent.Set(time.Now())
		}()

		sinceLastCall := time.Since(v.lastWaitForEvent.Get())
		timeToWait := waitForEventDelay - sinceLastCall
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(timeToWait):
		}
	}

	// Wait until we are allowed to build a block.
	{
		minTime := minNextBlockTime(v.VM.GetPreference().Header())
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(time.Until(minTime)):
		}
	}

	return v.VM.WaitForEvent(ctx)
}

// minNextBlockTime calculates the minimum next block time based on the header.
func minNextBlockTime(h *types.Header) time.Time {
	e := customtypes.GetHeaderExtra(h)
	// If the parent header has no min delay excess, there is nothing to wait
	// for, because the rule does not apply to the block to be built.
	if e.MinDelayExcess == nil {
		return time.Time{}
	}

	mde := *e.MinDelayExcess
	// delay excess is already verified by consensus so this can not overflow.
	delay := time.Duration(mde.Delay()) * time.Millisecond
	return saehook.BlockTimeFrom(h.Time, e.TimeMilliseconds).Add(delay)
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
