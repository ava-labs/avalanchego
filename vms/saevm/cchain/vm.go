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
	"time"

	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"github.com/ava-labs/libevm/triedb"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/evm/utils/rpc"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/vms/evm/database"
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

// VM wraps an [sae.VM] with the cross-chain pieces specific to the C-Chain.
type VM struct {
	*sae.VM // created by [VM.Initialize]

	// gossip frequencies are configurable to speed up testing.
	pullGossipPeriod time.Duration
	pushGossipPeriod time.Duration

	ctx          *snow.Context
	state        *state.State
	txpool       *txpool.Txpool
	gossipSet    *gossip.BloomSet[*gossipTx]
	pushGossiper *gossip.PushGossiper[*gossipTx]

	// onClose are executed in reverse order during [VM.Shutdown]. If a resource
	// depends on another resource, it MUST be added AFTER the resource it
	// depends on.
	onClose []func(context.Context) error
}

var ethDBPrefix = []byte("ethdb")

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

	userConfig, err := ParseConfig(configBytes)
	if err != nil {
		return fmt.Errorf("parsing user config: %w", err)
	}
	warpMessages, err := userConfig.WarpMessages()
	if err != nil {
		return fmt.Errorf("parsing warp messages: %w", err)
	}

	// [prefixdb.NewNested] is used because coreth used to be run as a plugin.
	// This meant that the database's prefix was not compacted, because the
	// provided database was wrapped by the rpcchainvm.
	ethDB := rawdb.NewDatabase(database.New(prefixdb.NewNested(ethDBPrefix, avaDB)))
	trieDBConfig := triedb.HashDefaults
	trieDB := triedb.NewDatabase(ethDB, trieDBConfig)

	genesis, err := parseGenesis(snowCtx, genesisBytes)
	if err != nil {
		return fmt.Errorf("parsing genesis: %w", err)
	}
	// On a fresh chain the last-accepted block is genesis itself.
	chainConfig, _, err := core.SetupGenesisBlock(ethDB, trieDB, genesis, genesis.ToBlock().Hash(), false)
	if err != nil {
		return fmt.Errorf("setting up genesis block: %w", err)
	}

	vm.state, err = state.New(snowCtx, avaDB)
	if err != nil {
		return fmt.Errorf("creating cchain state: %w", err)
	}
	vm.onClose = append(vm.onClose, func(context.Context) error {
		return vm.state.Close()
	})

	pendingTxs := txpool.NewPending()
	warpStorage := saewarp.NewStorage(avaDB, warpMessages...)
	hooks := newHooks(
		snowCtx,
		vm.state,
		chainConfig,
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
	vm.VM, err = sae.NewVM(ctx, hooks, saeConfig, snowCtx, chainConfig, ethDB, genesis.ToBlock(), appSender)
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

	if err := vm.AddHandler(p2p.AtomicTxGossipHandlerID, gossipHandler); err != nil {
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

	warpVerifier := saewarp.NewVerifier(&blockClient{vm: vm.VM}, warpStorage)
	warpHandler := acp118.NewCachedHandler(
		lru.NewCache[ids.ID, []byte](warpSignatureCacheSize),
		warpVerifier,
		snowCtx.WarpSigner,
	)
	if err := vm.AddHandler(p2p.SignatureRequestHandlerID, warpHandler); err != nil {
		return fmt.Errorf("registering warp signature handler: %w", err)
	}

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

// WaitForEvent waits for a transaction to be in the txpool or for the SAE VM to
// produce an event.
func (vm *VM) WaitForEvent(ctx context.Context) (common.Message, error) {
	// TODO(StephenButtolph): Do not busy loop with [common.PendingTxs]. The
	// txpools are cleared after block execution, so we may still have
	// transactions in the txpool while blocks containing those transactions are
	// processing.

	// TODO(StephenButtolph): Wait until the minimum block delay has passed.

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

// blockClient adapts [sae.VM] to the [saewarp.Backend] interface.
type blockClient struct {
	vm *sae.VM
}

var _ saewarp.Backend = (*blockClient)(nil)

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
