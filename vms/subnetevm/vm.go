// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnetevm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync/atomic"
	"time"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/triedb"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/evm/database"
	"github.com/ava-labs/avalanchego/vms/subnetevm/hook"
	"github.com/ava-labs/avalanchego/vms/subnetevm/hook/acp176"
	"github.com/ava-labs/avalanchego/vms/subnetevm/state"

	avadb "github.com/ava-labs/avalanchego/database"
	subnetevmparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	saewarp "github.com/ava-labs/avalanchego/vms/subnetevm/warp"

	// Force-load precompiles to trigger registration
	_ "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/registry" // Force-load precompiles to trigger registration
)

// VM is a harness around an [sae.VM], providing an `Initialize`
// method that supports being asynchronous since genesis or after a previously
// accepted synchronous block.
type VM struct {
	*sae.VM // created by [SinceGenesis.Initialize]
	config  sae.Config

	ctx *snow.Context
	db  avadb.Database

	// TODO(alarso16): remove later
	hooks *hook.Points

	// onClose are executed in reverse order during [SinceGenesis.Shutdown].
	// If a resource depends on another resource, it MUST be added AFTER the
	// resource it depends on.
	onClose []func()

	preference       atomic.Pointer[blocks.Block]
	lastWaitForEvent utils.Atomic[time.Time]
}

// New constructs a new [VM].
func New(c sae.Config) *VM {
	return &VM{
		config: c,
	}
}

const warpSignatureCacheSize = 512

var ethDBPrefix = []byte("ethdb")

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
) error {
	// [prefixdb.NewNested] is used because coreth used to be run as a plugin.
	// This meant that the database's prefix was not compacted, because the
	// provided database was wrapped by the rpcchainvm.
	db := rawdb.NewDatabase(database.New(prefixdb.NewNested(ethDBPrefix, avaDB)))
	tdb := triedb.NewDatabase(db, v.config.DBConfig.TrieDBConfig)

	snowCtx.Log.Info("parsing genesis")

	genesis, err := parseGenesis(snowCtx, genesisBytes, upgradeBytes)
	if err != nil {
		return fmt.Errorf("json.Unmarshal(%T): %w", genesis, err)
	}

	snowCtx.Log.Info("establishing last synchronous block")

	var lastSync *types.Block
	lastSyncBytes, err := state.ReadLastSync(avaDB)
	switch {
	case err == nil:
		lastSync = new(types.Block)
		if err := rlp.DecodeBytes(lastSyncBytes, lastSync); err != nil {
			return fmt.Errorf("rlp.DecodeBytes(..., %T): %w", lastSync, err)
		}
	case errors.Is(err, avadb.ErrNotFound):
		lastSync = genesis.ToBlock()
	default:
		return err
	}

	snowCtx.Log.Info("setting up the genesis",
		zap.Stringer("lastID", ids.ID(lastSync.Hash())),
		zap.Uint64("lastHeight", lastSync.NumberU64()),
	)

	// TODO: Are these reasonable?
	config, _, err := core.SetupGenesisBlock(db, tdb, genesis, lastSync.Hash(), false)
	if err != nil {
		return fmt.Errorf("core.SetupGenesisBlock(...): %w", err)
	}

	snowCtx.Log.Info("parsing user config")

	userConfig, err := ParseConfig(configBytes)
	if err != nil {
		return err
	}

	snowCtx.Log.Info("parsing warp message overrides")

	warpMessages, err := userConfig.WarpMessages()
	if err != nil {
		return err
	}

	var desiredDelayExcess *acp226.DelayExcess
	if userConfig.MinDelayTarget != nil {
		desiredDelayExcess = new(acp226.DelayExcess)
		*desiredDelayExcess = acp226.DesiredDelayExcess(*userConfig.MinDelayTarget)
	}
	var desiredTargetExcess *acp176.TargetExcess
	if userConfig.GasTarget != nil {
		desiredTargetExcess = new(acp176.TargetExcess)
		*desiredTargetExcess = acp176.DesiredTargetExcess(*userConfig.GasTarget)
	}

	warpStorage := saewarp.NewStorage(avaDB, warpMessages...)
	hooks := hook.NewPoints(
		snowCtx,
		avaDB,
		config,
		v.config.Now,
		desiredDelayExcess,
		desiredTargetExcess,
		warpStorage,
	)

	snowCtx.Log.Info("constructing the sae VM")

	inner, err := sae.NewVM(ctx, hooks, v.config, snowCtx, config, db, lastSync, appSender)
	if err != nil {
		return err
	}
	v.VM = inner
	v.ctx = snowCtx
	v.db = avaDB
	v.hooks = hooks

	snowCtx.Log.Info("registering subnetevm metrics")

	metrics := prometheus.NewRegistry()
	if err := snowCtx.Metrics.Register("subnetevm", metrics); err != nil {
		return fmt.Errorf("failed to register metrics: %w", err)
	}

	snowCtx.Log.Info("warp handlers")

	{ // ==========  Warp Handler  ==========
		warpVerifier := saewarp.NewVerifier(&blockClient{vm: inner}, warpStorage)
		warpHandler := acp118.NewCachedHandler(
			lru.NewCache[ids.ID, []byte](warpSignatureCacheSize),
			warpVerifier,
			snowCtx.WarpSigner,
		)
		if err := inner.AddHandler(p2p.SignatureRequestHandlerID, warpHandler); err != nil {
			return fmt.Errorf("network.AddHandler(warp): %w", err)
		}
	}

	snowCtx.Log.Info("initialized saevm")

	return nil
}

func parseGenesis(ctx *snow.Context, genesisBytes []byte, upgradeBytes []byte) (*core.Genesis, error) {
	g := new(core.Genesis)
	if err := json.Unmarshal(genesisBytes, g); err != nil {
		return nil, fmt.Errorf("parsing genesis: %w", err)
	}

	// Populate the Avalanche config extras.
	configExtra := subnetevmparams.GetExtra(g.Config)
	configExtra.AvalancheContext = extras.AvalancheContext{
		SnowCtx: ctx,
	}
	configExtra.NetworkUpgrades = extras.GetNetworkUpgrades(ctx.NetworkUpgrades)

	// Set network upgrade defaults
	configExtra.SetDefaults(ctx.NetworkUpgrades)

	// Apply upgradeBytes (if any) by unmarshalling them into [chainConfig.UpgradeConfig].
	// Initializing the chain will verify upgradeBytes are compatible with existing values.
	// This should be called before configExtra.Verify().
	if len(upgradeBytes) > 0 {
		var upgradeConfig extras.UpgradeConfig
		if err := json.Unmarshal(upgradeBytes, &upgradeConfig); err != nil {
			return nil, fmt.Errorf("failed to parse upgrade bytes: %w", err)
		}
		configExtra.UpgradeConfig = upgradeConfig
	}

	if configExtra.UpgradeConfig.NetworkUpgradeOverrides != nil {
		overrides := configExtra.UpgradeConfig.NetworkUpgradeOverrides
		marshaled, err := json.Marshal(overrides)
		if err != nil {
			log.Warn("Failed to marshal network upgrade overrides", "error", err, "overrides", overrides)
		} else {
			log.Info("Applying network upgrade overrides", "overrides", string(marshaled))
		}
		configExtra.Override(overrides)
	}

	if err := configExtra.Verify(); err != nil {
		return nil, fmt.Errorf("invalid chain config: %w", err)
	}

	// Align all the Ethereum upgrades to the Avalanche upgrades
	if err := subnetevmparams.SetEthUpgrades(g.Config); err != nil {
		return nil, fmt.Errorf("setting eth upgrades: %w", err)
	}
	return g, nil
}

func (v *VM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	return v.VM.CreateHandlers(ctx)
}

func (v *VM) SetPreference(ctx context.Context, id ids.ID, bCtx *block.Context) error {
	b, err := v.GetBlock(ctx, id)
	if err != nil {
		return err
	}
	v.preference.Store(b)
	return v.VM.SetPreference(ctx, id, bCtx)
}

// Prevent busy looping when the chain is more advanced than the mempool.
const waitForEventDelay = 100 * time.Millisecond

var errNoPreference = errors.New("no preferred block")

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
		parent := v.preference.Load()
		if parent == nil {
			return 0, errNoPreference
		}

		minTime := minNextBlockTime(parent.Header())
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
	return customtypes.BlockTime(h).Add(delay)
}

func (v *VM) Shutdown(ctx context.Context) error {
	for _, f := range slices.Backward(v.onClose) {
		f()
	}
	if v.VM == nil {
		return nil
	}
	return v.VM.Shutdown(ctx)
}

// blockClient adapts [sae.VM] to the [saewarp.BlockClient] interface.
type blockClient struct {
	vm *sae.VM
}

var _ saewarp.BlockClient = (*blockClient)(nil)

func (c *blockClient) IsAccepted(ctx context.Context, blockID ids.ID) error {
	b, err := c.vm.GetBlock(ctx, blockID)
	if err != nil {
		return err
	}
	acceptedID, err := c.vm.GetBlockIDAtHeight(ctx, b.Height())
	if err != nil {
		return err
	}
	if acceptedID != blockID {
		return avadb.ErrNotFound
	}
	return nil
}
