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

	_ "embed"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/evm/utils/rpc"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/evm/database"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/state"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/txpool"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"

	avadb "github.com/ava-labs/avalanchego/database"
	corethparams "github.com/ava-labs/avalanchego/graft/coreth/params"
	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	ethcommon "github.com/ava-labs/libevm/common"
	ethparams "github.com/ava-labs/libevm/params"
)

// VM wraps an [sae.VM] with the cross-chain pieces specific to the C-Chain.
type VM struct {
	*sae.VM // created by [VM.Initialize]

	// gossip frequencies are configurable to speed up testing.
	pullGossipPeriod time.Duration
	pushGossipPeriod time.Duration

	// now is the clock provided to the [sae.VM] and is used for block building.
	now func() time.Time

	ctx          *snow.Context
	chainConfig  *ethparams.ChainConfig
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
	_ []*snowcommon.Fx,
	appSender snowcommon.AppSender,
) (retErr error) {
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, vm.Shutdown(ctx))
		}
	}()

	vm.ctx = snowCtx

	// TODO(StephenButtolph): Allow minimal user configuration via configBytes.
	_ = configBytes

	// [prefixdb.NewNested] is used because coreth used to be run as a plugin.
	// This meant that the database's prefix was not compacted, because the
	// provided database was wrapped by the rpcchainvm.
	ethDB := rawdb.NewDatabase(database.New(prefixdb.NewNested(ethDBPrefix, avaDB)))
	trieDBConfig := triedb.HashDefaults

	genesis, err := parseGenesis(snowCtx, genesisBytes)
	if err != nil {
		return fmt.Errorf("parsing genesis: %w", err)
	}
	vm.chainConfig = genesis.Config

	genesisBlock, err := genesis.setup(ethDB, trieDBConfig)
	if err != nil {
		return fmt.Errorf("setting up genesis: %w", err)
	}

	vm.state, err = state.New(snowCtx, avaDB)
	if err != nil {
		return fmt.Errorf("creating cchain state: %w", err)
	}
	vm.onClose = append(vm.onClose, func(context.Context) error {
		return vm.state.Close()
	})

	pendingTxs := txpool.NewPending()
	hooks := newHooks(
		snowCtx,
		vm.state,
		pendingTxs,
		vm.now,
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
		Now: vm.now,
	}
	vm.VM, err = sae.NewVM(ctx, hooks, saeConfig, snowCtx, vm.chainConfig, ethDB, genesisBlock, appSender)
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
//
// Genesis (block 0) always uses the pre-ApricotPhase1 extData rules regardless
// of chain config: its legacy header left ExtDataHash empty, and bootstrapping
// re-parses the full ancestry including genesis, so it must be accepted here.
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
		actualHash     = customtypes.CalcExtDataHash(extData)
		wantHeaderHash = actualHash
	)
	if eth.NumberU64() == 0 || !corethparams.GetExtra(vm.chainConfig).IsApricotPhase1(eth.Time()) {
		wantHeaderHash = ethcommon.Hash{}
		wantHash := customtypes.EmptyExtDataHash
		if want, ok := extDataHashes[vm.ctx.NetworkID][eth.NumberU64()]; ok {
			wantHash = want
		}
		if actualHash != wantHash {
			return nil, fmt.Errorf("%w: have %x, want %x", errExtDataUnexpectedHash, actualHash, wantHash)
		}
	}
	if got := customtypes.GetHeaderExtra(eth.Header()).ExtDataHash; got != wantHeaderHash {
		return nil, fmt.Errorf("%w: have %x, want %x", errExtDataHashMismatch, got, wantHeaderHash)
	}
	return b, nil
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
func (vm *VM) WaitForEvent(ctx context.Context) (snowcommon.Message, error) {
	// TODO(StephenButtolph): Do not busy loop with [snowcommon.PendingTxs]. The
	// txpools are cleared after block execution, so we may still have
	// transactions in the txpool while blocks containing those transactions are
	// processing.

	// TODO(StephenButtolph): Wait until the minimum block delay has passed.

	ctx, cancel := context.WithCancel(ctx)
	type result struct {
		msg snowcommon.Message
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
		results <- result{snowcommon.PendingTxs, err}
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
