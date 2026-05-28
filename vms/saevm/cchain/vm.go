// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package cchain implements the C-Chain VM atop [sae.VM]. It composes the
// C-Chain block-building hooks, the cross-chain transaction pool, and the avax
// JSON-RPC service that ingests Export and Import transactions.
package cchain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/triedb"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/evm/utils/rpc"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/evm/database"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/acp176"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/state"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/txpool"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"

	avadb "github.com/ava-labs/avalanchego/database"
	corethparams "github.com/ava-labs/avalanchego/graft/coreth/params"
	warpcontract "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	saewarp "github.com/ava-labs/avalanchego/vms/saevm/cchain/warp"
)

const warpSignatureCacheSize = 512

var ethDBPrefix = []byte("ethdb")

// VM wraps an [sae.VM] with the cross-chain pieces specific to the C-Chain.
type VM struct {
	*sae.VM // created by [VM.Initialize]

	// These are configurable to speed up testing.
	pullGossipPeriod      time.Duration
	pushGossipPeriod      time.Duration
	initialMinDelayExcess acp226.DelayExcess

	ctx          *snow.Context
	state        *state.State
	txpool       *txpool.Txpool
	gossipSet    *gossip.BloomSet[*gossipTx]
	pushGossiper *gossip.PushGossiper[*gossipTx]

	// TODO: Remove this.
	hooks *hooks

	// onClose are executed in reverse order during [VM.Shutdown]. If a resource
	// depends on another resource, it MUST be added AFTER the resource it
	// depends on.
	onClose []func(context.Context) error

	preference       atomic.Pointer[blocks.Block]
	lastWaitForEvent utils.Atomic[time.Time]
}

// Initialize initializes the VM.
func (vm *VM) Initialize(
	ctx context.Context,
	snowCtx *snow.Context,
	avaDB avadb.Database,
	genesisBytes []byte,
	_ []byte,
	configBytes []byte,
	_ []*common.Fx,
	appSender common.AppSender,
) (retErr error) {
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, vm.Shutdown(ctx))
		}
	}()

	vm.ctx = snowCtx

	// [prefixdb.NewNested] is used because coreth used to be run as a plugin.
	// This meant that the database's prefix was not compacted, because the
	// provided database was wrapped by the rpcchainvm.
	ethDB := rawdb.NewDatabase(database.New(prefixdb.NewNested(ethDBPrefix, avaDB)))
	trieDBConfig := triedb.HashDefaults
	trieDB := triedb.NewDatabase(ethDB, trieDBConfig)

	snowCtx.Log.Info("parsing genesis")
	genesis, err := parseGenesis(snowCtx, genesisBytes)
	if err != nil {
		return fmt.Errorf("parsing genesis: %w", err)
	}

	snowCtx.Log.Info("establishing last synchronous block")
	var lastSync *types.Block
	lastSyncBytes, err := state.ReadLastSync(avaDB)
	switch {
	case err == nil:
		lastSync = new(types.Block)
		if err := rlp.DecodeBytes(lastSyncBytes, lastSync); err != nil {
			return fmt.Errorf("decoding last sync block: %w", err)
		}
	case errors.Is(err, avadb.ErrNotFound):
		lastSync = genesis.ToBlock()
	default:
		return fmt.Errorf("reading last sync block: %w", err)
	}

	snowCtx.Log.Info("setting up genesis",
		zap.Stringer("lastID", ids.ID(lastSync.Hash())),
		zap.Uint64("lastHeight", lastSync.NumberU64()),
	)
	chainConfig, _, err := core.SetupGenesisBlock(ethDB, trieDB, genesis, lastSync.Hash(), false)
	if err != nil {
		return fmt.Errorf("setting up genesis block: %w", err)
	}

	snowCtx.Log.Info("constructing cross-chain state")
	vm.state, err = state.New(snowCtx, avaDB)
	if err != nil {
		return fmt.Errorf("creating cchain state: %w", err)
	}
	vm.onClose = append(vm.onClose, func(context.Context) error {
		return vm.state.Close()
	})

	snowCtx.Log.Info("parsing user config")
	userConfig, err := ParseConfig(configBytes)
	if err != nil {
		return fmt.Errorf("parsing user config: %w", err)
	}

	snowCtx.Log.Info("parsing warp message overrides")
	warpMessages, err := userConfig.WarpMessages()
	if err != nil {
		return fmt.Errorf("parsing warp messages: %w", err)
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

	pendingTxs := txpool.NewPending()
	warpStorage := saewarp.NewStorage(avaDB, warpMessages...)
	vm.hooks = newHooks(
		snowCtx,
		vm.state,
		chainConfig,
		vm.initialMinDelayExcess,
		desiredDelayExcess,
		desiredTargetExcess,
		pendingTxs,
		warpStorage,
	)

	mempoolConfig := legacypool.DefaultConfig
	// Treat all transactions equally regardless of submission source — no
	// preferential admission or pricing for locally-submitted txs.
	mempoolConfig.NoLocals = true
	saeConfig := sae.Config{
		MempoolConfig: mempoolConfig,
		DBConfig: saedb.Config{
			TrieDBConfig: trieDBConfig,
		},
	}

	snowCtx.Log.Info("constructing the sae VM")
	vm.VM, err = sae.NewVM(ctx, vm.hooks, saeConfig, snowCtx, chainConfig, ethDB, lastSync, appSender)
	if err != nil {
		return fmt.Errorf("creating SAE VM: %w", err)
	}
	vm.onClose = append(vm.onClose, vm.VM.Shutdown)

	const maxTxPoolSize = 1024
	vm.txpool, err = txpool.New(snowCtx, chainConfig, pendingTxs, vm.VM, maxTxPoolSize)
	if err != nil {
		return fmt.Errorf("creating txpool: %w", err)
	}
	vm.onClose = append(vm.onClose, func(context.Context) error {
		vm.txpool.Close()
		return nil
	})

	reg, err := metrics.MakeAndRegister(snowCtx.Metrics, "cchain")
	if err != nil {
		return fmt.Errorf("making metrics: %w", err)
	}
	bloomMetrics, err := bloom.NewMetrics("gossip_bloom", reg)
	if err != nil {
		return fmt.Errorf("creating gossip bloom metrics: %w", err)
	}
	vm.gossipSet, err = gossip.NewBloomSet(
		newGossipTxPool(vm.txpool),
		gossip.BloomSetConfig{
			Metrics: bloomMetrics,
		},
	)
	if err != nil {
		return fmt.Errorf("creating gossip bloom set: %w", err)
	}
	gossipHandler, pullGossiper, pushGossiper, err := gossip.NewSystem(
		snowCtx.NodeID,
		vm.Network,
		vm.ValidatorPeers,
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
	vm.pushGossiper = pushGossiper

	if err := vm.VM.AddHandler(p2p.AtomicTxGossipHandlerID, gossipHandler); err != nil {
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

	snowCtx.Log.Info("warp handlers")
	warpVerifier := saewarp.NewVerifier(&blockClient{vm: vm.VM}, warpStorage)
	warpHandler := acp118.NewCachedHandler(
		lru.NewCache[ids.ID, []byte](warpSignatureCacheSize),
		warpVerifier,
		snowCtx.WarpSigner,
	)
	if err := vm.VM.AddHandler(p2p.SignatureRequestHandlerID, warpHandler); err != nil {
		return fmt.Errorf("registering warp signature handler: %w", err)
	}

	snowCtx.Log.Info("initialized saevm")
	return nil
}

// parseGenesis decodes the genesis bytes into a [*core.Genesis] and populates
// the Avalanche-specific config extras (network upgrades, Warp precompile
// schedule, Ethereum upgrade alignment).
func parseGenesis(ctx *snow.Context, b []byte) (*core.Genesis, error) {
	g := new(core.Genesis)
	if err := json.Unmarshal(b, g); err != nil {
		return nil, fmt.Errorf("unmarshalling genesis: %w", err)
	}

	configExtra := corethparams.GetExtra(g.Config)
	configExtra.AvalancheContext = extras.AvalancheContext{
		SnowCtx: ctx,
	}
	configExtra.NetworkUpgrades = extras.GetNetworkUpgrades(ctx.NetworkUpgrades)

	// If Durango is scheduled, schedule the Warp Precompile at the same time.
	if configExtra.DurangoBlockTimestamp != nil {
		configExtra.PrecompileUpgrades = append(configExtra.PrecompileUpgrades, extras.PrecompileUpgrade{
			Config: warpcontract.NewDefaultConfig(configExtra.DurangoBlockTimestamp),
		})
	}
	if err := configExtra.Verify(); err != nil {
		return nil, fmt.Errorf("invalid chain config: %w", err)
	}

	// Align the Ethereum upgrades to the Avalanche upgrades.
	if err := corethparams.SetEthUpgrades(g.Config); err != nil {
		return nil, fmt.Errorf("aligning Ethereum upgrades: %w", err)
	}
	return g, nil
}

const (
	avaxServiceName       = "avax"
	avaxHTTPExtensionPath = "/" + avaxServiceName
)

// CreateHandlers returns the HTTP handlers exposed by the underlying SAE VM
// augmented with the avax service at [avaxHTTPExtensionPath].
func (vm *VM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	m, err := vm.VM.CreateHandlers(ctx)
	if err != nil {
		return nil, fmt.Errorf("creating SAE handlers: %w", err)
	}

	service, err := newService(vm.ctx, vm.gossipSet, vm.pushGossiper, vm.state)
	if err != nil {
		return nil, fmt.Errorf("creating avax service: %w", err)
	}
	handler, err := rpc.NewHandler(avaxServiceName, service)
	if err != nil {
		return nil, fmt.Errorf("creating avax RPC handler: %w", err)
	}

	m[avaxHTTPExtensionPath] = handler
	return m, nil
}

func (vm *VM) SetPreference(ctx context.Context, id ids.ID, bCtx *block.Context) error {
	b, err := vm.GetBlock(ctx, id)
	if err != nil {
		return err
	}
	vm.preference.Store(b)
	return vm.VM.SetPreference(ctx, id, bCtx)
}

// Prevent busy looping when the chain is more advanced than the mempool.
const waitForEventDelay = 100 * time.Millisecond

var errNoPreference = errors.New("no preferred block")

// WaitForEvent blocks until the SAE VM emits an event or the cross-chain
// txpool has a pending transaction.
func (vm *VM) WaitForEvent(ctx context.Context) (common.Message, error) {
	// Throttle to avoid busy looping if we appear ready to build but keep
	// encountering errors.
	{
		defer func() {
			vm.lastWaitForEvent.Set(time.Now())
		}()

		sinceLastCall := time.Since(vm.lastWaitForEvent.Get())
		timeToWait := waitForEventDelay - sinceLastCall
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(timeToWait):
		}
	}

	// Wait until we are allowed to build a block on top of the preference.
	{
		parent := vm.preference.Load()
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

	ctx, cancel := context.WithCancel(ctx)
	type result struct {
		msg common.Message
		err error
	}
	results := make(chan result, 2)
	go func() {
		defer cancel()
		msg, err := vm.VM.WaitForEvent(ctx)
		results <- result{msg, err}
	}()
	go func() {
		defer cancel()
		err := vm.txpool.AwaitTxs(ctx)
		results <- result{common.PendingTxs, err}
	}()

	r := <-results
	return r.msg, r.err
}

// minNextBlockTime returns the earliest wall-clock time at which a child of h
// is allowed by h's [acp226.DelayExcess].
func minNextBlockTime(h *types.Header) time.Time {
	e := customtypes.GetHeaderExtra(h)
	if e.MinDelayExcess == nil {
		return time.Time{}
	}

	mde := *e.MinDelayExcess
	delay := time.Duration(mde.Delay()) * time.Millisecond //#nosec G115 -- delay excess is verified by consensus
	return customtypes.BlockTime(h).Add(delay)
}

// Shutdown releases every resource allocated by [VM.Initialize] in reverse
// order.
//
// It is idempotent and safe to call after a partially-failed [VM.Initialize].
func (vm *VM) Shutdown(ctx context.Context) error {
	errs := make([]error, len(vm.onClose))
	for i, f := range slices.Backward(vm.onClose) {
		errs[i] = f(ctx)
	}
	vm.onClose = nil
	return errors.Join(errs...)
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
