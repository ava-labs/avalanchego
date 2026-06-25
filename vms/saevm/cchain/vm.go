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
	"slices"
	"sync"
	"time"

	"github.com/ava-labs/libevm/ethdb"
	"github.com/prometheus/client_golang/prometheus"

	_ "embed"

	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/state"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/txpool"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp"
	"github.com/ava-labs/avalanchego/vms/saevm/network"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
	"github.com/ava-labs/avalanchego/vms/saevm/types"

	apimetrics "github.com/ava-labs/avalanchego/api/metrics"
	avadb "github.com/ava-labs/avalanchego/database"
	corethparams "github.com/ava-labs/avalanchego/graft/coreth/params"
	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	cchainsync "github.com/ava-labs/avalanchego/vms/saevm/cchain/statesync"
	ethcommon "github.com/ava-labs/libevm/common"
	ethparams "github.com/ava-labs/libevm/params"
)

var _ adaptor.ChainVM[*blocks.Block] = (*VM)(nil)

// VM wraps an [sae.VM] with the cross-chain pieces specific to the C-Chain.
type VM struct {
	*sae.VM                    // created by [VM.SetState] as bootstrapping or normal op
	*network.Network           // created by [VM.Initialize]
	*cchainsync.SummaryHandler // created by [VM.Initialize]

	// gossip frequencies are configurable to speed up testing.
	pullGossipPeriod time.Duration
	pushGossipPeriod time.Duration

	// now is the clock provided to the [sae.VM] and is used for block building.
	now func() time.Time

	ctx          *snow.Context
	chainConfig  *ethparams.ChainConfig
	state        *state.State
	metrics      *metrics
	txpool       *txpool.Txpool
	gossipSet    *gossip.BloomSet[*gossipTx]
	pushGossiper *gossip.PushGossiper[*gossipTx]

	mode                utils.Atomic[snow.State]
	deferredArgs        *deferredInit
	onBootstrappingOnce sync.Once
	handlers            handlerMap

	// onClose are executed in reverse order during [VM.Shutdown]. If a resource
	// depends on another resource, it MUST be added AFTER the resource it
	// depends on.
	onClose []func(context.Context) error

	lastWaitForEvent utils.Atomic[time.Time]
}

type deferredInit struct {
	g           *genesis
	db          ethdb.Database
	cfg         sae.Config
	hooks       *hooks
	pendingTxs  *txpool.Pending
	warpStorage *warp.Storage
	registry    *prometheus.Registry
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
	_ []*snowcommon.Fx,
	appSender snowcommon.AppSender,
) (retErr error) {
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, vm.Shutdown(ctx))
		}
	}()

	vm.ctx = snowCtx

	userConfig, err := parseConfig(configBytes, snowCtx.NetworkID)
	if err != nil {
		return fmt.Errorf("parsing user config: %w", err)
	}
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

	pendingTxs := txpool.NewPending()
	warpStorage := warp.NewStorage(avaDB, warpMessages...)
	hooks := newHooks(
		snowCtx,
		vm.state,
		vm.chainConfig,
		pendingTxs,
		warpStorage,
		vm.now,
		userConfig.desired(),
		vm.metrics,
	)
	vm.Network, err = network.New(snowCtx, appSender)
	if err != nil {
		return fmt.Errorf("creating network: %w", err)
	}

	// [prefixdb.NewNested] is used because coreth used to be run as a plugin.
	// This meant that the database's prefix was not compacted, because the
	// provided database was wrapped by the rpcchainvm.
	ethDB := types.NewEthDB(prefixdb.NewNested(ethDBPrefix, avaDB))

	if err := genesis.setup(ethDB); err != nil {
		return fmt.Errorf("writing genesis block: %w", err)
	}
	vm.SummaryHandler, err = cchainsync.New(userConfig.stateSyncConfig(), ethDB, hooks, vm.state, snowCtx.Log)
	if err != nil {
		return fmt.Errorf("creating summary handler: %w", err)
	}
	vm.onClose = append(vm.onClose, vm.SummaryHandler.Shutdown)

	vm.deferredArgs = &deferredInit{
		g:           genesis,
		db:          ethDB,
		cfg:         userConfig.saeConfig(vm.now),
		hooks:       hooks,
		pendingTxs:  pendingTxs,
		warpStorage: warpStorage,
		registry:    reg,
	}
	vm.handlers = make(handlerMap)
	return nil
}

// onBootstrapping finishes initializing the VM after all necessary state is available.
// This MUST be called exactly once, guaranteed using VM.onBootstrappingOnce.
func (vm *VM) onBootstrapping(ctx context.Context) error {
	var (
		genesis     = vm.deferredArgs.g
		ethDB       = vm.deferredArgs.db
		saeConfig   = vm.deferredArgs.cfg
		snowCtx     = vm.ctx
		hooks       = vm.deferredArgs.hooks
		pendingTxs  = vm.deferredArgs.pendingTxs
		warpStorage = vm.deferredArgs.warpStorage
		reg         = vm.deferredArgs.registry
	)

	tdbConfig := saeConfig.DBConfig.TrieDBConfig(snowCtx.ChainDataDir, snowCtx.Log)
	if err := genesis.checkAndWriteState(ethDB, tdbConfig); err != nil {
		return fmt.Errorf("setting up genesis: %w", err)
	}

	var err error
	vm.VM, err = sae.NewVM(ctx, hooks, saeConfig, snowCtx, vm.chainConfig, ethDB, vm.Network)
	if err != nil {
		return fmt.Errorf("creating SAE VM: %w", err)
	}
	vm.onClose = append(vm.onClose, vm.VM.Shutdown)

	const maxTxPoolSize = 1024
	vm.txpool, err = txpool.New(snowCtx, vm.chainConfig, pendingTxs, vm.VM, maxTxPoolSize)
	if err != nil {
		return fmt.Errorf("creating txpool: %w", err)
	}
	vm.onClose = append(vm.onClose, func(context.Context) error {
		vm.txpool.Close()
		return nil
	})

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
	vm.pushGossiper = pushGossiper

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

	if err := vm.updateHandlers(ctx); err != nil {
		return fmt.Errorf("updating HTTP handlers: %w", err)
	}

	return nil
}

var (
	// errInvalidBlockVersion is returned by [VM.ParseBlock] when a block's
	// BlockBodyExtra carries a Version other than 0, the only supported version.
	errInvalidBlockVersion = errors.New("invalid block version")
	// errExtDataUnexpectedHash is returned by [VM.ParseBlock] when a block's
	// extData does not correspond to the hardcoded ExtDataHash.
	errExtDataUnexpectedHash = errors.New("extData hash does not match expected value")
	// errExtDataHashMismatch is returned by [VM.ParseBlock] when a block's
	// extData does not hash to the ExtDataHash committed in its header.
	errExtDataHashMismatch = errors.New("extData hash does not match header")

	//go:embed extdata-fuji.json
	fujiExtDataHashes []byte
	//go:embed extdata-mainnet.json
	mainnetExtDataHashes []byte
	extDataHashes        map[uint32]map[uint64]ethcommon.Hash
)

func init() {
	mainnet := make(map[uint64]ethcommon.Hash)
	if err := json.Unmarshal(mainnetExtDataHashes, &mainnet); err != nil {
		panic(fmt.Errorf("unmarshalling extdata-mainnet.json: %w", err))
	}
	fuji := make(map[uint64]ethcommon.Hash)
	if err := json.Unmarshal(fujiExtDataHashes, &fuji); err != nil {
		panic(fmt.Errorf("unmarshalling extdata-fuji.json: %w", err))
	}
	extDataHashes = map[uint32]map[uint64]ethcommon.Hash{
		constants.MainnetID: mainnet,
		constants.FujiID:    fuji,
	}
}

// ParseBlock parses buf via the embedded SAE VM and additionally performs the
// C-Chain syntactic checks that the SAE VM is unaware of: that the block's
// BlockBodyExtra Version is 0 (the only supported version) and that its extData
// matches the ExtDataHash committed in the header.
//
// The block ID is the header hash. The header neither hashes the body's Version
// nor its extData bytes (it commits only ExtDataHash), so a block with a
// tampered Version or extData keeps the same ID. This override is the boundary
// that rejects such blocks before they are accepted, persisted, or executed.
func (vm *VM) ParseBlock(ctx context.Context, buf []byte) (*blocks.Block, error) {
	b, err := vm.VM.ParseBlock(ctx, buf)
	if err != nil {
		return nil, err
	}

	eth := b.EthBlock()
	if version := customtypes.BlockVersion(eth); version != 0 {
		return nil, fmt.Errorf("%w: %d", errInvalidBlockVersion, version)
	}

	var (
		extData        = customtypes.BlockExtData(eth)
		calculatedHash = customtypes.CalcExtDataHash(extData)
		wantHeaderHash = calculatedHash
	)
	// For genesis and pre-ApricotPhase1 blocks, the header's ExtDataHash is
	// expected to be empty with the actual data expected to be committed to in
	// [extDataHashes].
	if height := eth.NumberU64(); height == 0 || !corethparams.GetExtra(vm.chainConfig).IsApricotPhase1(eth.Time()) {
		wantHash := customtypes.EmptyExtDataHash
		if want, ok := extDataHashes[vm.ctx.NetworkID][height]; ok {
			wantHash = want
		}
		if calculatedHash != wantHash {
			return nil, fmt.Errorf("%w: have %x, want %x", errExtDataUnexpectedHash, calculatedHash, wantHash)
		}
		wantHeaderHash = ethcommon.Hash{}
	}
	if got := customtypes.GetHeaderExtra(eth.Header()).ExtDataHash; got != wantHeaderHash {
		return nil, fmt.Errorf("%w: have %x, want %x", errExtDataHashMismatch, got, wantHeaderHash)
	}
	return b, nil
}

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
