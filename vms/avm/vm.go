// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"container/list"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/gorilla/rpc/v2"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/pubsub"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/index"
	"github.com/ava-labs/avalanchego/vms/components/keystore"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	cjson "github.com/ava-labs/avalanchego/utils/json"
	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	batchTimeout       = time.Second
	batchSize          = 30
	assetToFxCacheSize = 1024
)

var (
	errIncompatibleFx            = errors.New("incompatible feature extension")
	errUnknownFx                 = errors.New("unknown feature extension")
	errGenesisAssetMustHaveState = errors.New("genesis asset must have non-empty state")
	errBootstrapping             = errors.New("chain is currently bootstrapping")
	errInsufficientFunds         = errors.New("insufficient funds")

	_ vertex.DAGVM = &VM{}
)

// VM implements the avalanche.DAGVM interface
type VM struct {
	Factory
	metrics
	avax.AddressManager
	avax.AtomicUTXOManager
	ids.Aliaser

	// Contains information of where this VM is executing
	ctx *snow.Context

	// Used to check local time
	clock mockable.Clock

	genesisCodec codec.Manager
	codec        codec.Manager

	pubsub *pubsub.Server

	// State management
	state State

	// Set to true once this VM is marked as `Bootstrapped` by the engine
	bootstrapped bool

	// asset id that will be used for fees
	feeAssetID ids.ID

	// Asset ID --> Bit set with fx IDs the asset supports
	assetToFxCache *cache.LRU

	// Transaction issuing
	timer        *timer.Timer
	batchTimeout time.Duration
	txs          []snowstorm.Tx
	toEngine     chan<- common.Message

	baseDB database.Database
	db     *versiondb.Database

	typeToFxIndex map[reflect.Type]int
	fxs           []*parsedFx

	walletService WalletService

	addressTxsIndexer index.AddressTxsIndexer
}

func (vm *VM) Connected(nodeID ids.ShortID, nodeVersion version.Application) error {
	return nil
}

func (vm *VM) Disconnected(nodeID ids.ShortID) error {
	return nil
}

/*
 ******************************************************************************
 ******************************** Avalanche API *******************************
 ******************************************************************************
 */

type Config struct {
	IndexTransactions    bool `json:"index-transactions"`
	IndexAllowIncomplete bool `json:"index-allow-incomplete"`
}

// Initialize implements the avalanche.DAGVM interface
func (vm *VM) Initialize(
	ctx *snow.Context,
	dbManager manager.Manager,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngine chan<- common.Message,
	fxs []*common.Fx,
	_ common.AppSender,
) error {
	avmConfig := Config{}
	if len(configBytes) > 0 {
		if err := json.Unmarshal(configBytes, &avmConfig); err != nil {
			return err
		}
		ctx.Log.Info("VM config initialized %+v", avmConfig)
	}

	registerer := prometheus.NewRegistry()
	if err := ctx.Metrics.Register(registerer); err != nil {
		return err
	}

	err := vm.metrics.Initialize("", registerer)
	if err != nil {
		return err
	}
	vm.AddressManager = avax.NewAddressManager(ctx)
	vm.Aliaser = ids.NewAliaser()

	db := dbManager.Current().Database
	vm.ctx = ctx
	vm.toEngine = toEngine
	vm.baseDB = db
	vm.db = versiondb.New(db)
	vm.assetToFxCache = &cache.LRU{Size: assetToFxCacheSize}

	vm.pubsub = pubsub.New(ctx.NetworkID, ctx.Log)

	typedFxs := make([]Fx, len(fxs))
	vm.fxs = make([]*parsedFx, len(fxs))
	for i, fxContainer := range fxs {
		if fxContainer == nil {
			return errIncompatibleFx
		}
		fx, ok := fxContainer.Fx.(Fx)
		if !ok {
			return errIncompatibleFx
		}
		typedFxs[i] = fx
		vm.fxs[i] = &parsedFx{
			ID: fxContainer.ID,
			Fx: fx,
		}
	}

	vm.typeToFxIndex = map[reflect.Type]int{}
	vm.genesisCodec, vm.codec, err = newCustomCodecs(
		vm.typeToFxIndex,
		&vm.clock,
		ctx.Log,
		typedFxs,
	)
	if err != nil {
		return err
	}

	vm.AtomicUTXOManager = avax.NewAtomicUTXOManager(ctx.SharedMemory, vm.codec)

	state, err := NewMeteredState(vm.db, vm.genesisCodec, vm.codec, registerer)
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

	vm.walletService.vm = vm
	vm.walletService.pendingTxMap = make(map[ids.ID]*list.Element)
	vm.walletService.pendingTxOrdering = list.New()

	// use no op impl when disabled in config
	if avmConfig.IndexTransactions {
		vm.ctx.Log.Info("address transaction indexing is enabled")
		vm.addressTxsIndexer, err = index.NewIndexer(vm.db, vm.ctx.Log, "", registerer, avmConfig.IndexAllowIncomplete)
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
	return vm.db.Commit()
}

// onBootstrapStarted is called by the consensus engine when it starts bootstrapping this chain
func (vm *VM) onBootstrapStarted() error {
	for _, fx := range vm.fxs {
		if err := fx.Fx.Bootstrapping(); err != nil {
			return err
		}
	}
	return nil
}

func (vm *VM) onNormalOperationsStarted() error {
	for _, fx := range vm.fxs {
		if err := fx.Fx.Bootstrapped(); err != nil {
			return err
		}
	}
	vm.bootstrapped = true
	return nil
}

func (vm *VM) SetState(state snow.State) error {
	switch state {
	case snow.Bootstrapping:
		return vm.onBootstrapStarted()
	case snow.NormalOp:
		return vm.onNormalOperationsStarted()
	default:
		return snow.ErrUnknownState
	}
}

// Shutdown implements the avalanche.DAGVM interface
func (vm *VM) Shutdown() error {
	if vm.timer == nil {
		return nil
	}

	// There is a potential deadlock if the timer is about to execute a timeout.
	// So, the lock must be released before stopping the timer.
	vm.ctx.Lock.Unlock()
	vm.timer.Stop()
	vm.ctx.Lock.Lock()

	return vm.baseDB.Close()
}

// Get implements the avalanche.DAGVM interface
func (vm *VM) Version() (string, error) {
	return version.Current.String(), nil
}

// CreateHandlers implements the avalanche.DAGVM interface
func (vm *VM) CreateHandlers() (map[string]*common.HTTPHandler, error) {
	codec := cjson.NewCodec()

	rpcServer := rpc.NewServer()
	rpcServer.RegisterCodec(codec, "application/json")
	rpcServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	rpcServer.RegisterInterceptFunc(vm.metrics.apiRequestMetric.InterceptRequest)
	rpcServer.RegisterAfterFunc(vm.metrics.apiRequestMetric.AfterRequest)
	// name this service "avm"
	if err := rpcServer.RegisterService(&Service{vm: vm}, "avm"); err != nil {
		return nil, err
	}

	walletServer := rpc.NewServer()
	walletServer.RegisterCodec(codec, "application/json")
	walletServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	walletServer.RegisterInterceptFunc(vm.metrics.apiRequestMetric.InterceptRequest)
	walletServer.RegisterAfterFunc(vm.metrics.apiRequestMetric.AfterRequest)
	// name this service "wallet"
	err := walletServer.RegisterService(&vm.walletService, "wallet")

	return map[string]*common.HTTPHandler{
		"":        {Handler: rpcServer},
		"/wallet": {Handler: walletServer},
		"/events": {LockOptions: common.NoLock, Handler: vm.pubsub},
	}, err
}

// CreateStaticHandlers implements the common.StaticVM interface
func (vm *VM) CreateStaticHandlers() (map[string]*common.HTTPHandler, error) {
	newServer := rpc.NewServer()
	codec := cjson.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")

	// name this service "avm"
	staticService := CreateStaticService()
	return map[string]*common.HTTPHandler{
		"": {LockOptions: common.WriteLock, Handler: newServer},
	}, newServer.RegisterService(staticService, "avm")
}

// Pending implements the avalanche.DAGVM interface
func (vm *VM) PendingTxs() []snowstorm.Tx {
	vm.timer.Cancel()

	txs := vm.txs
	vm.txs = nil
	return txs
}

// Parse implements the avalanche.DAGVM interface
func (vm *VM) ParseTx(b []byte) (snowstorm.Tx, error) {
	return vm.parseTx(b)
}

// Get implements the avalanche.DAGVM interface
func (vm *VM) GetTx(txID ids.ID) (snowstorm.Tx, error) {
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
	genesis := Genesis{}
	if _, err := vm.genesisCodec.Unmarshal(genesisBytes, &genesis); err != nil {
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

		tx := Tx{
			UnsignedTx: &genesisTx.CreateAssetTx,
		}
		if err := tx.SignSECP256K1Fx(vm.genesisCodec, nil); err != nil {
			return err
		}

		txID := tx.ID()

		if err := vm.Alias(txID, genesisTx.Alias); err != nil {
			return err
		}

		if !stateInitialized {
			if err := vm.initState(tx); err != nil {
				return err
			}
		}
		if index == 0 {
			vm.ctx.Log.Info("Fee payments are using Asset with Alias: %s, AssetID: %s", genesisTx.Alias, txID)
			vm.feeAssetID = txID
		}
	}

	if !stateInitialized {
		return vm.state.SetInitialized()
	}

	return nil
}

func (vm *VM) initState(tx Tx) error {
	txID := tx.ID()
	vm.ctx.Log.Info("initializing with AssetID %s", txID)
	if err := vm.state.PutTx(txID, &tx); err != nil {
		return err
	}
	if err := vm.state.PutStatus(txID, choices.Accepted); err != nil {
		return err
	}
	for _, utxo := range tx.UTXOs() {
		if err := vm.state.PutUTXO(utxo.InputID(), utxo); err != nil {
			return err
		}
	}
	return nil
}

func (vm *VM) parseTx(bytes []byte) (*UniqueTx, error) {
	rawTx, err := vm.parsePrivateTx(bytes)
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
		if err := vm.state.PutTx(tx.ID(), tx.Tx); err != nil {
			return nil, err
		}
		if err := tx.setStatus(choices.Processing); err != nil {
			return nil, err
		}
		return tx, vm.db.Commit()
	}

	return tx, nil
}

func (vm *VM) parsePrivateTx(txBytes []byte) (*Tx, error) {
	tx := &Tx{}
	_, err := vm.codec.Unmarshal(txBytes, tx)
	if err != nil {
		return nil, err
	}
	unsignedBytes, err := vm.codec.Marshal(codecVersion, &tx.UnsignedTx)
	if err != nil {
		return nil, err
	}
	tx.Initialize(unsignedBytes, txBytes)
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

func (vm *VM) getUTXO(utxoID *avax.UTXOID) (*avax.UTXO, error) {
	inputID := utxoID.InputID()
	utxo, err := vm.state.GetUTXO(inputID)
	if err == nil {
		return utxo, nil
	}

	inputTx, inputIndex := utxoID.InputSource()
	parent := UniqueTx{
		vm:   vm,
		txID: inputTx,
	}

	if err := parent.verifyWithoutCacheWrites(); err != nil {
		return nil, errMissingUTXO
	} else if status := parent.Status(); status.Decided() {
		return nil, errMissingUTXO
	}

	parentUTXOs := parent.UTXOs()
	if uint32(len(parentUTXOs)) <= inputIndex || int(inputIndex) < 0 {
		return nil, errInvalidUTXO
	}
	return parentUTXOs[int(inputIndex)], nil
}

func (vm *VM) getFx(val interface{}) (int, error) {
	valType := reflect.TypeOf(val)
	fx, exists := vm.typeToFxIndex[valType]
	if !exists {
		return 0, errUnknownFx
	}
	return fx, nil
}

// getParsedFx returns the parsedFx object for a given TransferableInput
// or TransferableOutput object
func (vm *VM) getParsedFx(val interface{}) (*parsedFx, error) {
	idx, err := vm.getFx(val)
	if err != nil {
		return nil, err
	}

	fx := vm.fxs[idx]
	return fx, nil
}

func (vm *VM) verifyFxUsage(fxID int, assetID ids.ID) bool {
	// Check cache to see whether this asset supports this fx
	fxIDsIntf, assetInCache := vm.assetToFxCache.Get(assetID)
	if assetInCache {
		return fxIDsIntf.(ids.BitSet).Contains(uint(fxID))
	}
	// Caches doesn't say whether this asset support this fx.
	// Get the tx that created the asset and check.
	tx := &UniqueTx{
		vm:   vm,
		txID: assetID,
	}
	if status := tx.Status(); !status.Fetched() {
		return false
	}
	createAssetTx, ok := tx.UnsignedTx.(*CreateAssetTx)
	if !ok {
		// This transaction was not an asset creation tx
		return false
	}
	fxIDs := ids.BitSet(0)
	for _, state := range createAssetTx.States {
		if state.FxIndex == uint32(fxID) {
			// Cache that this asset supports this fx
			fxIDs.Add(uint(fxID))
		}
	}
	vm.assetToFxCache.Put(assetID, fxIDs)
	return fxIDs.Contains(uint(fxID))
}

func (vm *VM) verifyTransferOfUTXO(tx UnsignedTx, in *avax.TransferableInput, cred verify.Verifiable, utxo *avax.UTXO) error {
	fxIndex, err := vm.getFx(cred)
	if err != nil {
		return err
	}
	fx := vm.fxs[fxIndex].Fx

	utxoAssetID := utxo.AssetID()
	inAssetID := in.AssetID()
	if utxoAssetID != inAssetID {
		return errAssetIDMismatch
	}

	if !vm.verifyFxUsage(fxIndex, inAssetID) {
		return errIncompatibleFx
	}

	return fx.VerifyTransfer(tx, in.In, cred, utxo.Out)
}

func (vm *VM) verifyTransfer(tx UnsignedTx, in *avax.TransferableInput, cred verify.Verifiable) error {
	utxo, err := vm.getUTXO(&in.UTXOID)
	if err != nil {
		return err
	}
	return vm.verifyTransferOfUTXO(tx, in, cred, utxo)
}

func (vm *VM) verifyOperation(tx UnsignedTx, op *Operation, cred verify.Verifiable) error {
	opAssetID := op.AssetID()

	numUTXOs := len(op.UTXOIDs)
	utxos := make([]interface{}, numUTXOs)
	for i, utxoID := range op.UTXOIDs {
		utxo, err := vm.getUTXO(utxoID)
		if err != nil {
			return err
		}

		utxoAssetID := utxo.AssetID()
		if utxoAssetID != opAssetID {
			return errAssetIDMismatch
		}
		utxos[i] = utxo.Out
	}

	fxIndex, err := vm.getFx(op.Op)
	if err != nil {
		return err
	}
	fx := vm.fxs[fxIndex].Fx

	if !vm.verifyFxUsage(fxIndex, opAssetID) {
		return errIncompatibleFx
	}
	return fx.VerifyOperation(tx, op.Op, cred, utxos)
}

// LoadUser returns:
// 1) The UTXOs that reference one or more addresses controlled by the given user
// 2) A keychain that contains this user's keys
// If [addrsToUse] has positive length, returns UTXOs that reference one or more
// addresses controlled by the given user that are also in [addrsToUse].
func (vm *VM) LoadUser(
	username string,
	password string,
	addrsToUse ids.ShortSet,
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

func (vm *VM) Spend(
	utxos []*avax.UTXO,
	kc *secp256k1fx.Keychain,
	amounts map[ids.ID]uint64,
) (
	map[ids.ID]uint64,
	[]*avax.TransferableInput,
	[][]*crypto.PrivateKeySECP256K1R,
	error,
) {
	amountsSpent := make(map[ids.ID]uint64, len(amounts))
	time := vm.clock.Unix()

	ins := []*avax.TransferableInput{}
	keys := [][]*crypto.PrivateKeySECP256K1R{}
	for _, utxo := range utxos {
		assetID := utxo.AssetID()
		amount := amounts[assetID]
		amountSpent := amountsSpent[assetID]

		if amountSpent >= amount {
			// we already have enough inputs allocated to this asset
			continue
		}

		inputIntf, signers, err := kc.Spend(utxo.Out, time)
		if err != nil {
			// this utxo can't be spent with the current keys right now
			continue
		}
		input, ok := inputIntf.(avax.TransferableIn)
		if !ok {
			// this input doesn't have an amount, so I don't care about it here
			continue
		}
		newAmountSpent, err := safemath.Add64(amountSpent, input.Amount())
		if err != nil {
			// there was an error calculating the consumed amount, just error
			return nil, nil, nil, errSpendOverflow
		}
		amountsSpent[assetID] = newAmountSpent

		// add the new input to the array
		ins = append(ins, &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  avax.Asset{ID: assetID},
			In:     input,
		})
		// add the required keys to the array
		keys = append(keys, signers)
	}

	for asset, amount := range amounts {
		if amountsSpent[asset] < amount {
			return nil, nil, nil, fmt.Errorf("want to spend %d of asset %s but only have %d",
				amount,
				asset,
				amountsSpent[asset],
			)
		}
	}

	avax.SortTransferableInputsWithSigners(ins, keys)
	return amountsSpent, ins, keys, nil
}

func (vm *VM) SpendNFT(
	utxos []*avax.UTXO,
	kc *secp256k1fx.Keychain,
	assetID ids.ID,
	groupID uint32,
	to ids.ShortID,
) (
	[]*Operation,
	[][]*crypto.PrivateKeySECP256K1R,
	error,
) {
	time := vm.clock.Unix()

	ops := []*Operation{}
	keys := [][]*crypto.PrivateKeySECP256K1R{}

	for _, utxo := range utxos {
		// makes sure that the variable isn't overwritten with the next iteration
		utxo := utxo

		if len(ops) > 0 {
			// we have already been able to create the operation needed
			break
		}

		if utxo.AssetID() != assetID {
			// wrong asset ID
			continue
		}
		out, ok := utxo.Out.(*nftfx.TransferOutput)
		if !ok {
			// wrong output type
			continue
		}
		if out.GroupID != groupID {
			// wrong group id
			continue
		}
		indices, signers, ok := kc.Match(&out.OutputOwners, time)
		if !ok {
			// unable to spend the output
			continue
		}

		// add the new operation to the array
		ops = append(ops, &Operation{
			Asset:   utxo.Asset,
			UTXOIDs: []*avax.UTXOID{&utxo.UTXOID},
			Op: &nftfx.TransferOperation{
				Input: secp256k1fx.Input{
					SigIndices: indices,
				},
				Output: nftfx.TransferOutput{
					GroupID: out.GroupID,
					Payload: out.Payload,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{to},
					},
				},
			},
		})
		// add the required keys to the array
		keys = append(keys, signers)
	}

	if len(ops) == 0 {
		return nil, nil, errInsufficientFunds
	}

	sortOperationsWithSigners(ops, keys, vm.codec)
	return ops, keys, nil
}

func (vm *VM) SpendAll(
	utxos []*avax.UTXO,
	kc *secp256k1fx.Keychain,
) (
	map[ids.ID]uint64,
	[]*avax.TransferableInput,
	[][]*crypto.PrivateKeySECP256K1R,
	error,
) {
	amountsSpent := make(map[ids.ID]uint64)
	time := vm.clock.Unix()

	ins := []*avax.TransferableInput{}
	keys := [][]*crypto.PrivateKeySECP256K1R{}
	for _, utxo := range utxos {
		assetID := utxo.AssetID()
		amountSpent := amountsSpent[assetID]

		inputIntf, signers, err := kc.Spend(utxo.Out, time)
		if err != nil {
			// this utxo can't be spent with the current keys right now
			continue
		}
		input, ok := inputIntf.(avax.TransferableIn)
		if !ok {
			// this input doesn't have an amount, so I don't care about it here
			continue
		}
		newAmountSpent, err := safemath.Add64(amountSpent, input.Amount())
		if err != nil {
			// there was an error calculating the consumed amount, just error
			return nil, nil, nil, errSpendOverflow
		}
		amountsSpent[assetID] = newAmountSpent

		// add the new input to the array
		ins = append(ins, &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  avax.Asset{ID: assetID},
			In:     input,
		})
		// add the required keys to the array
		keys = append(keys, signers)
	}

	avax.SortTransferableInputsWithSigners(ins, keys)
	return amountsSpent, ins, keys, nil
}

func (vm *VM) Mint(
	utxos []*avax.UTXO,
	kc *secp256k1fx.Keychain,
	amounts map[ids.ID]uint64,
	to ids.ShortID,
) (
	[]*Operation,
	[][]*crypto.PrivateKeySECP256K1R,
	error,
) {
	time := vm.clock.Unix()

	ops := []*Operation{}
	keys := [][]*crypto.PrivateKeySECP256K1R{}

	for _, utxo := range utxos {
		// makes sure that the variable isn't overwritten with the next iteration
		utxo := utxo

		assetID := utxo.AssetID()
		amount := amounts[assetID]
		if amount == 0 {
			continue
		}

		out, ok := utxo.Out.(*secp256k1fx.MintOutput)
		if !ok {
			continue
		}

		inIntf, signers, err := kc.Spend(out, time)
		if err != nil {
			continue
		}

		in, ok := inIntf.(*secp256k1fx.Input)
		if !ok {
			continue
		}

		// add the operation to the array
		ops = append(ops, &Operation{
			Asset:   utxo.Asset,
			UTXOIDs: []*avax.UTXOID{&utxo.UTXOID},
			Op: &secp256k1fx.MintOperation{
				MintInput:  *in,
				MintOutput: *out,
				TransferOutput: secp256k1fx.TransferOutput{
					Amt: amount,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{to},
					},
				},
			},
		})
		// add the required keys to the array
		keys = append(keys, signers)

		// remove the asset from the required amounts to mint
		delete(amounts, assetID)
	}

	for _, amount := range amounts {
		if amount > 0 {
			return nil, nil, errAddressesCantMintAsset
		}
	}

	sortOperationsWithSigners(ops, keys, vm.codec)
	return ops, keys, nil
}

func (vm *VM) MintNFT(
	utxos []*avax.UTXO,
	kc *secp256k1fx.Keychain,
	assetID ids.ID,
	payload []byte,
	to ids.ShortID,
) (
	[]*Operation,
	[][]*crypto.PrivateKeySECP256K1R,
	error,
) {
	time := vm.clock.Unix()

	ops := []*Operation{}
	keys := [][]*crypto.PrivateKeySECP256K1R{}

	for _, utxo := range utxos {
		// makes sure that the variable isn't overwritten with the next iteration
		utxo := utxo

		if len(ops) > 0 {
			// we have already been able to create the operation needed
			break
		}

		if utxo.AssetID() != assetID {
			// wrong asset id
			continue
		}
		out, ok := utxo.Out.(*nftfx.MintOutput)
		if !ok {
			// wrong output type
			continue
		}

		indices, signers, ok := kc.Match(&out.OutputOwners, time)
		if !ok {
			// unable to spend the output
			continue
		}

		// add the operation to the array
		ops = append(ops, &Operation{
			Asset: avax.Asset{ID: assetID},
			UTXOIDs: []*avax.UTXOID{
				&utxo.UTXOID,
			},
			Op: &nftfx.MintOperation{
				MintInput: secp256k1fx.Input{
					SigIndices: indices,
				},
				GroupID: out.GroupID,
				Payload: payload,
				Outputs: []*secp256k1fx.OutputOwners{{
					Threshold: 1,
					Addrs:     []ids.ShortID{to},
				}},
			},
		})
		// add the required keys to the array
		keys = append(keys, signers)
	}

	if len(ops) == 0 {
		return nil, nil, errAddressesCantMintAsset
	}

	sortOperationsWithSigners(ops, keys, vm.codec)
	return ops, keys, nil
}

// selectChangeAddr returns the change address to be used for [kc] when [changeAddr] is given
// as the optional change address argument
func (vm *VM) selectChangeAddr(defaultAddr ids.ShortID, changeAddr string) (ids.ShortID, error) {
	if changeAddr == "" {
		return defaultAddr, nil
	}
	addr, err := vm.ParseLocalAddress(changeAddr)
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

// This VM doesn't (currently) have any app-specific messages
func (vm *VM) AppRequest(nodeID ids.ShortID, requestID uint32, deadline time.Time, request []byte) error {
	return nil
}

// This VM doesn't (currently) have any app-specific messages
func (vm *VM) AppResponse(nodeID ids.ShortID, requestID uint32, response []byte) error {
	return nil
}

// This VM doesn't (currently) have any app-specific messages
func (vm *VM) AppRequestFailed(nodeID ids.ShortID, requestID uint32) error {
	return nil
}

// This VM doesn't (currently) have any app-specific messages
func (vm *VM) AppGossip(nodeID ids.ShortID, msg []byte) error {
	return nil
}
