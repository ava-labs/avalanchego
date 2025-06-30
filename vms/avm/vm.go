// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sync"

	"github.com/gorilla/rpc/v2"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/linked"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/avm/block"
	"github.com/ava-labs/avalanchego/vms/avm/config"
	"github.com/ava-labs/avalanchego/vms/avm/network"
	"github.com/ava-labs/avalanchego/vms/avm/state"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/utxo"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/txs/mempool"

	blockbuilder "github.com/ava-labs/avalanchego/vms/avm/block/builder"
	blockexecutor "github.com/ava-labs/avalanchego/vms/avm/block/executor"
	extensions "github.com/ava-labs/avalanchego/vms/avm/fxs"
	avmmetrics "github.com/ava-labs/avalanchego/vms/avm/metrics"
	txexecutor "github.com/ava-labs/avalanchego/vms/avm/txs/executor"
)

var (
	errIncompatibleFx            = errors.New("incompatible feature extension")
	errUnknownFx                 = errors.New("unknown feature extension")
	errGenesisAssetMustHaveState = errors.New("genesis asset must have non-empty state")

	_ vertex.LinearizableVMWithEngine = (*VM)(nil)
)

type VM struct {
	network.Atomic

	config.Config

	metrics avmmetrics.Metrics

	avax.AddressManager
	ids.Aliaser
	utxo.Spender

	// Contains information of where this VM is executing
	ctx *snow.Context

	// Used to check local time
	clock mockable.Clock

	registerer prometheus.Registerer

	connectedPeers map[ids.NodeID]*version.Application

	parser block.Parser

	appSender common.AppSender

	// State management
	state state.State

	// asset id that will be used for fees
	feeAssetID ids.ID

	baseDB database.Database
	db     *versiondb.Database

	typeToFxIndex map[reflect.Type]int
	fxs           []*extensions.ParsedFx

	walletService WalletService

	txBackend *txexecutor.Backend

	// Cancelled on shutdown
	onShutdownCtx context.Context
	// Call [onShutdownCtxCancel] to cancel [onShutdownCtx] during Shutdown()
	onShutdownCtxCancel context.CancelFunc
	awaitShutdown       sync.WaitGroup

	networkConfig network.Config
	// These values are only initialized after the chain has been linearized.
	blockbuilder.Builder
	chainManager blockexecutor.Manager
	network      *network.Network
}

func (vm *VM) Connected(ctx context.Context, nodeID ids.NodeID, version *version.Application) error {
	// If the chain isn't linearized yet, we must track the peers externally
	// until the network is initialized.
	if vm.network == nil {
		vm.connectedPeers[nodeID] = version
		return nil
	}
	return vm.network.Connected(ctx, nodeID, version)
}

func (vm *VM) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	// If the chain isn't linearized yet, we must track the peers externally
	// until the network is initialized.
	if vm.network == nil {
		delete(vm.connectedPeers, nodeID)
		return nil
	}
	return vm.network.Disconnected(ctx, nodeID)
}

/*
 ******************************************************************************
 ********************************* Common VM **********************************
 ******************************************************************************
 */

func (vm *VM) Initialize(
	_ context.Context,
	ctx *snow.Context,
	db database.Database,
	genesisBytes []byte,
	_ []byte,
	configBytes []byte,
	fxs []*common.Fx,
	appSender common.AppSender,
) error {
	noopMessageHandler := common.NewNoOpAppHandler(ctx.Log)
	vm.Atomic = network.NewAtomic(noopMessageHandler)

	avmConfig, err := ParseConfig(configBytes)
	if err != nil {
		return err
	}
	ctx.Log.Info("VM config initialized",
		zap.Reflect("config", avmConfig),
	)

	vm.registerer, err = metrics.MakeAndRegister(ctx.Metrics, "")
	if err != nil {
		return err
	}

	vm.connectedPeers = make(map[ids.NodeID]*version.Application)

	// Initialize metrics as soon as possible
	vm.metrics, err = avmmetrics.New(vm.registerer)
	if err != nil {
		return fmt.Errorf("failed to initialize metrics: %w", err)
	}

	vm.AddressManager = avax.NewAddressManager(ctx)
	vm.Aliaser = ids.NewAliaser()

	vm.ctx = ctx
	vm.appSender = appSender
	vm.baseDB = db
	vm.db = versiondb.New(db)

	typedFxs := make([]extensions.Fx, len(fxs))
	vm.fxs = make([]*extensions.ParsedFx, len(fxs))
	for i, fxContainer := range fxs {
		if fxContainer == nil {
			return errIncompatibleFx
		}
		fx, ok := fxContainer.Fx.(extensions.Fx)
		if !ok {
			return errIncompatibleFx
		}
		typedFxs[i] = fx
		vm.fxs[i] = &extensions.ParsedFx{
			ID: fxContainer.ID,
			Fx: fx,
		}
	}

	vm.typeToFxIndex = map[reflect.Type]int{}
	vm.parser, err = block.NewCustomParser(
		vm.typeToFxIndex,
		&vm.clock,
		ctx.Log,
		typedFxs,
	)
	if err != nil {
		return err
	}

	codec := vm.parser.Codec()
	vm.Spender = utxo.NewSpender(&vm.clock, codec)

	state, err := state.New(
		vm.db,
		vm.parser,
		vm.registerer,
		avmConfig.ChecksumsEnabled,
	)
	if err != nil {
		return err
	}

	vm.state = state

	if err := vm.initGenesis(genesisBytes); err != nil {
		return err
	}

	vm.walletService.vm = vm
	vm.walletService.pendingTxs = linked.NewHashmap[ids.ID, *txs.Tx]()

	vm.txBackend = &txexecutor.Backend{
		Ctx:           ctx,
		Config:        &vm.Config,
		Fxs:           vm.fxs,
		TypeToFxIndex: vm.typeToFxIndex,
		Codec:         vm.parser.Codec(),
		FeeAssetID:    vm.feeAssetID,
		Bootstrapped:  false,
	}

	vm.onShutdownCtx, vm.onShutdownCtxCancel = context.WithCancel(context.Background())
	vm.networkConfig = avmConfig.Network
	return vm.state.Commit()
}

// onBootstrapStarted is called by the consensus engine when it starts bootstrapping this chain
func (vm *VM) onBootstrapStarted() error {
	vm.txBackend.Bootstrapped = false
	for _, fx := range vm.fxs {
		if err := fx.Fx.Bootstrapping(); err != nil {
			return err
		}
	}
	return nil
}

func (vm *VM) onNormalOperationsStarted() error {
	vm.txBackend.Bootstrapped = true
	for _, fx := range vm.fxs {
		if err := fx.Fx.Bootstrapped(); err != nil {
			return err
		}
	}
	return nil
}

func (vm *VM) SetState(_ context.Context, state snow.State) error {
	switch state {
	case snow.Bootstrapping:
		return vm.onBootstrapStarted()
	case snow.NormalOp:
		return vm.onNormalOperationsStarted()
	default:
		return snow.ErrUnknownState
	}
}

func (vm *VM) Shutdown(context.Context) error {
	if vm.state == nil {
		return nil
	}

	vm.onShutdownCtxCancel()
	vm.awaitShutdown.Wait()

	return errors.Join(
		vm.state.Close(),
		vm.baseDB.Close(),
	)
}

func (*VM) Version(context.Context) (string, error) {
	return version.Current.String(), nil
}

func (vm *VM) CreateHandlers(context.Context) (map[string]http.Handler, error) {
	codec := json.NewCodec()

	rpcServer := rpc.NewServer()
	rpcServer.RegisterCodec(codec, "application/json")
	rpcServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	rpcServer.RegisterInterceptFunc(vm.metrics.InterceptRequest)
	rpcServer.RegisterAfterFunc(vm.metrics.AfterRequest)
	// name this service "avm"
	if err := rpcServer.RegisterService(&Service{vm: vm}, "avm"); err != nil {
		return nil, err
	}

	walletServer := rpc.NewServer()
	walletServer.RegisterCodec(codec, "application/json")
	walletServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	walletServer.RegisterInterceptFunc(vm.metrics.InterceptRequest)
	walletServer.RegisterAfterFunc(vm.metrics.AfterRequest)
	// name this service "wallet"
	err := walletServer.RegisterService(&vm.walletService, "wallet")

	return map[string]http.Handler{
		"":        rpcServer,
		"/wallet": walletServer,
	}, err
}

func (*VM) CreateHTTP2Handler(context.Context) (http.Handler, error) {
	return nil, nil
}

/*
 ******************************************************************************
 ********************************** Chain VM **********************************
 ******************************************************************************
 */

func (vm *VM) GetBlock(_ context.Context, blkID ids.ID) (snowman.Block, error) {
	return vm.chainManager.GetBlock(blkID)
}

func (vm *VM) ParseBlock(_ context.Context, blkBytes []byte) (snowman.Block, error) {
	blk, err := vm.parser.ParseBlock(blkBytes)
	if err != nil {
		return nil, err
	}
	return vm.chainManager.NewBlock(blk), nil
}

func (vm *VM) SetPreference(_ context.Context, blkID ids.ID) error {
	vm.chainManager.SetPreference(blkID)
	return nil
}

func (vm *VM) LastAccepted(context.Context) (ids.ID, error) {
	return vm.chainManager.LastAccepted(), nil
}

func (vm *VM) GetBlockIDAtHeight(_ context.Context, height uint64) (ids.ID, error) {
	return vm.state.GetBlockIDAtHeight(height)
}

/*
 ******************************************************************************
 *********************************** DAG VM ***********************************
 ******************************************************************************
 */

func (vm *VM) Linearize(ctx context.Context, stopVertexID ids.ID) error {
	time := vm.Config.Upgrades.CortinaTime
	if err := vm.state.InitializeChainState(stopVertexID, time); err != nil {
		return fmt.Errorf("failed to initialize chain state: %w", err)
	}

	metrics, err := mempool.NewMetrics("mempool", vm.registerer)
	if err != nil {
		return fmt.Errorf("failed to create mempool metrics: %w", err)
	}
	mempool := mempool.New[*txs.Tx](metrics)
	vm.chainManager = blockexecutor.NewManager(
		mempool,
		vm.metrics,
		vm.state,
		vm.txBackend,
		&vm.clock,
		vm.onAccept,
	)

	vm.Builder = blockbuilder.New(
		vm.txBackend,
		vm.chainManager,
		&vm.clock,
		mempool,
	)

	// Invariant: The context lock is not held when calling network.IssueTx.
	vm.network, err = network.New(
		vm.ctx.Log,
		vm.ctx.NodeID,
		vm.ctx.SubnetID,
		vm.ctx.ValidatorState,
		vm.parser,
		network.NewLockedTxVerifier(
			&vm.ctx.Lock,
			vm.chainManager,
		),
		mempool,
		vm.appSender,
		vm.registerer,
		vm.networkConfig,
	)
	if err != nil {
		return fmt.Errorf("failed to initialize network: %w", err)
	}

	// Notify the network of our current peers
	for nodeID, version := range vm.connectedPeers {
		if err := vm.network.Connected(ctx, nodeID, version); err != nil {
			return err
		}
	}
	vm.connectedPeers = nil

	// Note: It's important only to switch the networking stack after the full
	// chainVM has been initialized. Traffic will immediately start being
	// handled asynchronously.
	vm.Atomic.Set(vm.network)

	vm.awaitShutdown.Add(2)
	go func() {
		defer vm.awaitShutdown.Done()

		// Invariant: PushGossip must never grab the context lock.
		vm.network.PushGossip(vm.onShutdownCtx)
	}()
	go func() {
		defer vm.awaitShutdown.Done()

		// Invariant: PullGossip must never grab the context lock.
		vm.network.PullGossip(vm.onShutdownCtx)
	}()

	return nil
}

func (vm *VM) ParseTx(_ context.Context, bytes []byte) (snowstorm.Tx, error) {
	tx, err := vm.parser.ParseTx(bytes)
	if err != nil {
		return nil, err
	}

	err = tx.Unsigned.Visit(&txexecutor.SyntacticVerifier{
		Backend: vm.txBackend,
		Tx:      tx,
	})
	if err != nil {
		return nil, err
	}

	return &Tx{
		vm: vm,
		tx: tx,
	}, nil
}

/*
 ******************************************************************************
 ********************************** JSON API **********************************
 ******************************************************************************
 */

// issueTxFromRPC attempts to send a transaction to consensus.
//
// Invariant: The context lock is not held
// Invariant: This function is only called after Linearize has been called.
func (vm *VM) issueTxFromRPC(tx *txs.Tx) (ids.ID, error) {
	txID := tx.ID()
	err := vm.network.IssueTxFromRPC(tx)
	if err != nil && !errors.Is(err, mempool.ErrDuplicateTx) {
		vm.ctx.Log.Debug("failed to add tx to mempool",
			zap.Stringer("txID", txID),
			zap.Error(err),
		)
		return txID, err
	}
	return txID, nil
}

/*
 ******************************************************************************
 ********************************** Helpers ***********************************
 ******************************************************************************
 */

func (vm *VM) initGenesis(genesisBytes []byte) error {
	genesisCodec := vm.parser.GenesisCodec()
	genesis := Genesis{}
	if _, err := genesisCodec.Unmarshal(genesisBytes, &genesis); err != nil {
		return err
	}

	stateInitialized, err := vm.state.IsInitialized()
	if err != nil {
		return err
	}

	// secure this by defaulting to avaxAsset
	vm.feeAssetID = vm.ctx.AVAXAssetID

	for index, genesisTx := range genesis.Txs {
		if len(genesisTx.Outs) != 0 {
			return errGenesisAssetMustHaveState
		}

		tx := &txs.Tx{
			Unsigned: &genesisTx.CreateAssetTx,
		}
		if err := tx.Initialize(genesisCodec); err != nil {
			return err
		}

		txID := tx.ID()
		if err := vm.Alias(txID, genesisTx.Alias); err != nil {
			return err
		}

		if !stateInitialized {
			vm.initState(tx)
		}
		if index == 0 {
			vm.ctx.Log.Info("fee asset is established",
				zap.String("alias", genesisTx.Alias),
				zap.Stringer("assetID", txID),
			)
			vm.feeAssetID = txID
		}
	}

	if !stateInitialized {
		return vm.state.SetInitialized()
	}

	return nil
}

func (vm *VM) initState(tx *txs.Tx) {
	txID := tx.ID()
	vm.ctx.Log.Info("initializing genesis asset",
		zap.Stringer("txID", txID),
	)
	vm.state.AddTx(tx)
	for _, utxo := range tx.UTXOs() {
		vm.state.AddUTXO(utxo)
	}
}

// lookupAssetID looks for an ID aliased by [asset] and if it fails
// attempts to parse [asset] into an ID
func (vm *VM) lookupAssetID(asset string) (ids.ID, error) {
	if assetID, err := vm.Lookup(asset); err == nil {
		return assetID, nil
	}
	if assetID, err := ids.FromString(asset); err == nil {
		return assetID, nil
	}
	return ids.Empty, fmt.Errorf("asset '%s' not found", asset)
}

// Invariant: onAccept is called when [tx] is being marked as accepted, but
// before its state changes are applied.
// TODO: Remove [onAccept] once the deprecated APIs this powers are removed.
func (vm *VM) onAccept(tx *txs.Tx) {
	vm.walletService.decided(tx.ID())
}
