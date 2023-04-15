// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	stdjson "encoding/json"

	"github.com/gorilla/rpc/v2"

	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/pubsub"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/avm/blocks"
	"github.com/ava-labs/avalanchego/vms/avm/config"
	"github.com/ava-labs/avalanchego/vms/avm/metrics"
	"github.com/ava-labs/avalanchego/vms/avm/network"
	"github.com/ava-labs/avalanchego/vms/avm/states"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/txs/mempool"
	"github.com/ava-labs/avalanchego/vms/avm/utxo"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/index"
	"github.com/ava-labs/avalanchego/vms/components/keystore"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	blockbuilder "github.com/ava-labs/avalanchego/vms/avm/blocks/builder"
	blockexecutor "github.com/ava-labs/avalanchego/vms/avm/blocks/executor"
	extensions "github.com/ava-labs/avalanchego/vms/avm/fxs"
	txexecutor "github.com/ava-labs/avalanchego/vms/avm/txs/executor"
)

const (
	batchTimeout       = time.Second
	batchSize          = 30
	assetToFxCacheSize = 1024
	txDeduplicatorSize = 8192
)

var (
	errIncompatibleFx            = errors.New("incompatible feature extension")
	errUnknownFx                 = errors.New("unknown feature extension")
	errGenesisAssetMustHaveState = errors.New("genesis asset must have non-empty state")
	errBootstrapping             = errors.New("chain is currently bootstrapping")

	_ vertex.LinearizableVMWithEngine = (*VM)(nil)
)

type VM struct {
	network.Atomic

	config.Config

	metrics metrics.Metrics

	avax.AddressManager
	avax.AtomicUTXOManager
	ids.Aliaser
	utxo.Spender

	// Contains information of where this VM is executing
	ctx *snow.Context

	// Used to check local time
	clock mockable.Clock

	registerer prometheus.Registerer

	parser blocks.Parser

	pubsub *pubsub.Server

	appSender common.AppSender

	// State management
	state states.State

	// Set to true once this VM is marked as `Bootstrapped` by the engine
	bootstrapped bool

	// asset id that will be used for fees
	feeAssetID ids.ID

	// Asset ID --> Bit set with fx IDs the asset supports
	assetToFxCache *cache.LRU[ids.ID, set.Bits64]

	// Transaction issuing
	timer        *timer.Timer
	batchTimeout time.Duration
	txs          []snowstorm.Tx
	toEngine     chan<- common.Message

	baseDB database.Database
	db     *versiondb.Database

	typeToFxIndex map[reflect.Type]int
	fxs           []*extensions.ParsedFx

	walletService WalletService

	addressTxsIndexer index.AddressTxsIndexer

	uniqueTxs cache.Deduplicator[ids.ID, *UniqueTx]

	txBackend *txexecutor.Backend
	dagState  *dagState

	// These values are only initialized after the chain has been linearized.
	blockbuilder.Builder
	chainManager blockexecutor.Manager
	network      network.Network
}

func (*VM) Connected(context.Context, ids.NodeID, *version.Application) error {
	return nil
}

func (*VM) Disconnected(context.Context, ids.NodeID) error {
	return nil
}

/*
 ******************************************************************************
 ********************************* Common VM **********************************
 ******************************************************************************
 */

type Config struct {
	IndexTransactions    bool `json:"index-transactions"`
	IndexAllowIncomplete bool `json:"index-allow-incomplete"`
}

func (vm *VM) Initialize(
	_ context.Context,
	ctx *snow.Context,
	dbManager manager.Manager,
	genesisBytes []byte,
	_ []byte,
	configBytes []byte,
	toEngine chan<- common.Message,
	fxs []*common.Fx,
	appSender common.AppSender,
) error {
	noopMessageHandler := common.NewNoOpAppHandler(ctx.Log)
	vm.Atomic = network.NewAtomic(noopMessageHandler)

	avmConfig := Config{}
	if len(configBytes) > 0 {
		if err := stdjson.Unmarshal(configBytes, &avmConfig); err != nil {
			return err
		}
		ctx.Log.Info("VM config initialized",
			zap.Reflect("config", avmConfig),
		)
	}

	registerer := prometheus.NewRegistry()
	if err := ctx.Metrics.Register(registerer); err != nil {
		return err
	}
	vm.registerer = registerer

	// Initialize metrics as soon as possible
	var err error
	vm.metrics, err = metrics.New("", registerer)
	if err != nil {
		return fmt.Errorf("failed to initialize metrics: %w", err)
	}

	vm.AddressManager = avax.NewAddressManager(ctx)
	vm.Aliaser = ids.NewAliaser()

	db := dbManager.Current().Database
	vm.ctx = ctx
	vm.toEngine = toEngine
	vm.appSender = appSender
	vm.baseDB = db
	vm.db = versiondb.New(db)
	vm.assetToFxCache = &cache.LRU[ids.ID, set.Bits64]{Size: assetToFxCacheSize}

	vm.pubsub = pubsub.New(ctx.Log)

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
	vm.parser, err = blocks.NewCustomParser(
		vm.typeToFxIndex,
		&vm.clock,
		ctx.Log,
		typedFxs,
	)
	if err != nil {
		return err
	}

	codec := vm.parser.Codec()
	vm.AtomicUTXOManager = avax.NewAtomicUTXOManager(ctx.SharedMemory, codec)
	vm.Spender = utxo.NewSpender(&vm.clock, codec)

	state, err := states.New(vm.db, vm.parser, vm.registerer)
	if err != nil {
		return err
	}

	vm.state = state

	if err := vm.initGenesis(genesisBytes); err != nil {
		return err
	}

	vm.timer = timer.NewTimer(func() {
		ctx.Lock.Lock()
		defer ctx.Lock.Unlock()

		vm.FlushTxs()
	})
	go ctx.Log.RecoverAndPanic(vm.timer.Dispatch)
	vm.batchTimeout = batchTimeout

	vm.uniqueTxs = &cache.EvictableLRU[ids.ID, *UniqueTx]{
		Size: txDeduplicatorSize,
	}
	vm.walletService.vm = vm
	vm.walletService.pendingTxs = linkedhashmap.New[ids.ID, *txs.Tx]()

	// use no op impl when disabled in config
	if avmConfig.IndexTransactions {
		vm.ctx.Log.Warn("deprecated address transaction indexing is enabled")
		vm.addressTxsIndexer, err = index.NewIndexer(vm.db, vm.ctx.Log, "", vm.registerer, avmConfig.IndexAllowIncomplete)
		if err != nil {
			return fmt.Errorf("failed to initialize address transaction indexer: %w", err)
		}
	} else {
		vm.ctx.Log.Info("address transaction indexing is disabled")
		vm.addressTxsIndexer, err = index.NewNoIndexer(vm.db, avmConfig.IndexAllowIncomplete)
		if err != nil {
			return fmt.Errorf("failed to initialize disabled indexer: %w", err)
		}
	}

	vm.txBackend = &txexecutor.Backend{
		Ctx:           ctx,
		Config:        &vm.Config,
		Fxs:           vm.fxs,
		TypeToFxIndex: vm.typeToFxIndex,
		Codec:         vm.parser.Codec(),
		FeeAssetID:    vm.feeAssetID,
		Bootstrapped:  false,
	}
	vm.dagState = &dagState{
		Chain: vm.state,
		vm:    vm,
	}

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

	txID, err := ids.FromString("2JPwx3rbUy877CWYhtXpfPVS5tD8KfnbiF5pxMRu6jCaq5dnME")
	if err != nil {
		return err
	}
	utxoID := avax.UTXOID{
		TxID:        txID,
		OutputIndex: 192,
	}
	vm.state.DeleteUTXO(utxoID.InputID())
	if err := vm.state.Commit(); err != nil {
		return err
	}

	vm.bootstrapped = true
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
	if vm.timer == nil {
		return nil
	}

	// There is a potential deadlock if the timer is about to execute a timeout.
	// So, the lock must be released before stopping the timer.
	vm.ctx.Lock.Unlock()
	vm.timer.Stop()
	vm.ctx.Lock.Lock()

	errs := wrappers.Errs{}
	errs.Add(
		vm.state.Close(),
		vm.baseDB.Close(),
	)
	return errs.Err
}

func (*VM) Version(context.Context) (string, error) {
	return version.Current.String(), nil
}

func (vm *VM) CreateHandlers(context.Context) (map[string]*common.HTTPHandler, error) {
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

	return map[string]*common.HTTPHandler{
		"":        {Handler: rpcServer},
		"/wallet": {Handler: walletServer},
		"/events": {LockOptions: common.NoLock, Handler: vm.pubsub},
	}, err
}

func (*VM) CreateStaticHandlers(context.Context) (map[string]*common.HTTPHandler, error) {
	newServer := rpc.NewServer()
	codec := json.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")

	// name this service "avm"
	staticService := CreateStaticService()
	return map[string]*common.HTTPHandler{
		"": {LockOptions: common.WriteLock, Handler: newServer},
	}, newServer.RegisterService(staticService, "avm")
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

/*
 ******************************************************************************
 *********************************** DAG VM ***********************************
 ******************************************************************************
 */

func (vm *VM) Linearize(_ context.Context, stopVertexID ids.ID, toEngine chan<- common.Message) error {
	time := version.GetCortinaTime(vm.ctx.NetworkID)
	err := vm.state.InitializeChainState(stopVertexID, time)
	if err != nil {
		return err
	}

	mempool, err := mempool.New("mempool", vm.registerer, toEngine)
	if err != nil {
		return fmt.Errorf("failed to create mempool: %w", err)
	}

	vm.chainManager = blockexecutor.NewManager(
		mempool,
		vm.metrics,
		&chainState{
			State: vm.state,
		},
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

	vm.network = network.New(
		vm.ctx,
		vm.parser,
		vm.chainManager,
		mempool,
		vm.appSender,
	)

	// Note: It's important only to switch the networking stack after the full
	// chainVM has been initialized. Traffic will immediately start being
	// handled asynchronously.
	vm.Atomic.Set(vm.network)
	return nil
}

func (vm *VM) PendingTxs(context.Context) []snowstorm.Tx {
	vm.timer.Cancel()

	txs := vm.txs
	vm.txs = nil
	return txs
}

func (vm *VM) ParseTx(_ context.Context, b []byte) (snowstorm.Tx, error) {
	return vm.parseTx(b)
}

func (vm *VM) GetTx(_ context.Context, txID ids.ID) (snowstorm.Tx, error) {
	tx := &UniqueTx{
		vm:   vm,
		txID: txID,
	}
	// Verify must be called in the case the that tx was flushed from the unique
	// cache.
	return tx, tx.verifyWithoutCacheWrites()
}

/*
 ******************************************************************************
 ********************************** JSON API **********************************
 ******************************************************************************
 */

// IssueTx attempts to send a transaction to consensus.
// If onDecide is specified, the function will be called when the transaction is
// either accepted or rejected with the appropriate status. This function will
// go out of scope when the transaction is removed from memory.
func (vm *VM) IssueTx(b []byte) (ids.ID, error) {
	if !vm.bootstrapped {
		return ids.ID{}, errBootstrapping
	}

	// If the chain has been linearized, issue the tx to the network.
	if vm.Builder != nil {
		tx, err := vm.parser.ParseTx(b)
		if err != nil {
			vm.ctx.Log.Debug("failed to parse tx",
				zap.Error(err),
			)
			return ids.ID{}, err
		}

		err = vm.network.IssueTx(context.TODO(), tx)
		if err != nil {
			vm.ctx.Log.Debug("failed to add tx to mempool",
				zap.Error(err),
			)
			return ids.ID{}, err
		}

		return tx.ID(), nil
	}

	// TODO: After the chain is linearized, remove the following code.
	tx, err := vm.parseTx(b)
	if err != nil {
		return ids.ID{}, err
	}
	if err := tx.verifyWithoutCacheWrites(); err != nil {
		return ids.ID{}, err
	}
	vm.issueTx(tx)
	return tx.ID(), nil
}

// TODO: After the chain is linearized, remove this.
func (vm *VM) issueStopVertex() error {
	select {
	case vm.toEngine <- common.StopVertex:
	default:
		vm.ctx.Log.Debug("dropping common.StopVertex message to engine due to contention")
	}
	return nil
}

/*
 ******************************************************************************
 ********************************** Timer API *********************************
 ******************************************************************************
 */

// FlushTxs into consensus
func (vm *VM) FlushTxs() {
	vm.timer.Cancel()
	if len(vm.txs) != 0 {
		select {
		case vm.toEngine <- common.PendingTxs:
		default:
			vm.ctx.Log.Debug("dropping message to engine due to contention")
			vm.timer.SetTimeoutIn(vm.batchTimeout)
		}
	}
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
		if err := vm.parser.InitializeGenesisTx(tx); err != nil {
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
	vm.state.AddStatus(txID, choices.Accepted)
	for _, utxo := range tx.UTXOs() {
		vm.state.AddUTXO(utxo)
	}
}

func (vm *VM) parseTx(bytes []byte) (*UniqueTx, error) {
	rawTx, err := vm.parser.ParseTx(bytes)
	if err != nil {
		return nil, err
	}

	tx := &UniqueTx{
		TxCachedState: &TxCachedState{
			Tx: rawTx,
		},
		vm:   vm,
		txID: rawTx.ID(),
	}
	if err := tx.SyntacticVerify(); err != nil {
		return nil, err
	}

	if tx.Status() == choices.Unknown {
		vm.state.AddTx(tx.Tx)
		tx.setStatus(choices.Processing)
		return tx, vm.state.Commit()
	}

	return tx, nil
}

func (vm *VM) issueTx(tx snowstorm.Tx) {
	vm.txs = append(vm.txs, tx)
	switch {
	case len(vm.txs) == batchSize:
		vm.FlushTxs()
	case len(vm.txs) == 1:
		vm.timer.SetTimeoutIn(vm.batchTimeout)
	}
}

// LoadUser returns:
// 1) The UTXOs that reference one or more addresses controlled by the given user
// 2) A keychain that contains this user's keys
// If [addrsToUse] has positive length, returns UTXOs that reference one or more
// addresses controlled by the given user that are also in [addrsToUse].
func (vm *VM) LoadUser(
	username string,
	password string,
	addrsToUse set.Set[ids.ShortID],
) (
	[]*avax.UTXO,
	*secp256k1fx.Keychain,
	error,
) {
	user, err := keystore.NewUserFromKeystore(vm.ctx.Keystore, username, password)
	if err != nil {
		return nil, nil, err
	}
	// Drop any potential error closing the database to report the original
	// error
	defer user.Close()

	kc, err := keystore.GetKeychain(user, addrsToUse)
	if err != nil {
		return nil, nil, err
	}

	utxos, err := avax.GetAllUTXOs(vm.state, kc.Addresses())
	if err != nil {
		return nil, nil, fmt.Errorf("problem retrieving user's UTXOs: %w", err)
	}

	return utxos, kc, user.Close()
}

// selectChangeAddr returns the change address to be used for [kc] when [changeAddr] is given
// as the optional change address argument
func (vm *VM) selectChangeAddr(defaultAddr ids.ShortID, changeAddr string) (ids.ShortID, error) {
	if changeAddr == "" {
		return defaultAddr, nil
	}
	addr, err := avax.ParseServiceAddress(vm, changeAddr)
	if err != nil {
		return ids.ShortID{}, fmt.Errorf("couldn't parse changeAddr: %w", err)
	}
	return addr, nil
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
	return ids.ID{}, fmt.Errorf("asset '%s' not found", asset)
}

// Invariant: onAccept is called when [tx] is being marked as accepted, but
// before its state changes are applied.
// Invariant: any error returned by onAccept should be considered fatal.
// TODO: Remove [onAccept] once the deprecated APIs this powers are removed.
func (vm *VM) onAccept(tx *txs.Tx) error {
	// Fetch the input UTXOs
	txID := tx.ID()
	inputUTXOIDs := tx.Unsigned.InputUTXOs()
	inputUTXOs := make([]*avax.UTXO, 0, len(inputUTXOIDs))
	for _, utxoID := range inputUTXOIDs {
		// Don't bother fetching the input UTXO if its symbolic
		if utxoID.Symbolic() {
			continue
		}

		utxo, err := vm.state.GetUTXOFromID(utxoID)
		if err == database.ErrNotFound {
			vm.ctx.Log.Debug("dropping utxo from index",
				zap.Stringer("txID", txID),
				zap.Stringer("utxoTxID", utxoID.TxID),
				zap.Uint32("utxoOutputIndex", utxoID.OutputIndex),
			)
			continue
		}
		if err != nil {
			// should never happen because the UTXO was previously verified to
			// exist
			return fmt.Errorf("error finding UTXO %s: %w", utxoID, err)
		}
		inputUTXOs = append(inputUTXOs, utxo)
	}

	outputUTXOs := tx.UTXOs()
	// index input and output UTXOs
	if err := vm.addressTxsIndexer.Accept(txID, inputUTXOs, outputUTXOs); err != nil {
		return fmt.Errorf("error indexing tx: %w", err)
	}

	vm.pubsub.Publish(NewPubSubFilterer(tx))
	vm.walletService.decided(txID)
	return nil
}

// UniqueTx de-duplicates the transaction.
func (vm *VM) DeduplicateTx(tx *UniqueTx) *UniqueTx {
	return vm.uniqueTxs.Deduplicate(tx)
}
