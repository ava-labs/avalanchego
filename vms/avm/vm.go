// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"bytes"
	"container/list"
	"errors"
	"fmt"
	"math"
	"reflect"
	"time"

	"github.com/gorilla/rpc/v2"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/hierarchycodec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/codec/reflectcodec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm/conflicts"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	cjson "github.com/ava-labs/avalanchego/utils/json"
	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	batchTimeout       = time.Second
	batchSize          = 30
	stateCacheSize     = 30000
	idCacheSize        = 30000
	txCacheSize        = 30000
	assetToFxCacheSize = 1024
	maxUTXOsToFetch    = 1024

	// Codec version used before AvalancheGo 1.1.0
	pre110CodecVersion = uint16(0)

	// Current codec version
	currentCodecVersion = uint16(1)
)

var (
	errIncompatibleFx            = errors.New("incompatible feature extension")
	errUnknownFx                 = errors.New("unknown feature extension")
	errGenesisAssetMustHaveState = errors.New("genesis asset must have non-empty state")
	errWrongBlockchainID         = errors.New("wrong blockchain ID")
	errBootstrapping             = errors.New("chain is currently bootstrapping")
	errInsufficientFunds         = errors.New("insufficient funds")
	errNoPermission              = errors.New("the given credential does not authorize transfer of this UTXO")

	_ vertex.DAGVM = &VM{}
)

// VM implements the avalanche.DAGVM interface
type VM struct {
	metrics
	ids.Aliaser

	// Contains information of where this VM is executing
	ctx *snow.Context

	// Used to check local time
	clock timer.Clock

	genesisCodec  codec.Manager
	codec         codec.Manager
	codecRegistry codec.Registry

	pubsub *cjson.PubSubServer

	// State management
	state *prefixedState

	// Set to true once this VM is marked as `Bootstrapped` by the engine
	bootstrapped bool

	// fee that must be burned by every state creating transaction
	creationTxFee uint64
	// fee that must be burned by every non-state creating transaction
	txFee uint64

	// Asset ID --> Bit set with fx IDs the asset supports
	assetToFxCache *cache.LRU

	// Transaction issuing
	timer        *timer.Timer
	batchTimeout time.Duration
	txs          []conflicts.Tx
	toEngine     chan<- common.Message

	baseDB database.Database
	db     *versiondb.Database

	typeToFxIndex map[reflect.Type]int
	fxs           []*parsedFx

	walletService WalletService
}

/*
 ******************************************************************************
 ******************************** Avalanche API *******************************
 ******************************************************************************
 */

// Initialize implements the avalanche.DAGVM interface
func (vm *VM) Initialize(
	ctx *snow.Context,
	db database.Database,
	genesisBytes []byte,
	toEngine chan<- common.Message,
	fxs []*common.Fx,
) error {
	vm.ctx = ctx
	vm.toEngine = toEngine
	vm.baseDB = db
	vm.db = versiondb.New(db)
	vm.typeToFxIndex = map[reflect.Type]int{}
	vm.Aliaser.Initialize()
	vm.assetToFxCache = &cache.LRU{Size: assetToFxCacheSize}

	vm.pubsub = cjson.NewPubSubServer(ctx)

	gc, err := pre110Codec(1 << 20)
	if err != nil {
		return fmt.Errorf("couldn't create genesis codec: %w", err)
	}
	vm.genesisCodec = codec.NewManager(math.MaxUint32)
	if err := vm.genesisCodec.RegisterCodec(pre110CodecVersion, gc); err != nil {
		return fmt.Errorf("couldn't create genesis codec manager: %w", err)
	}

	vm.codec = codec.NewDefaultManager()
	pre110Codec, err := pre110Codec(linearcodec.DefaultMaxSliceLength)
	if err != nil {
		return fmt.Errorf("couldn't create pre-1.1.0 codec: %w", err)
	}
	if err := vm.codec.RegisterCodec(pre110CodecVersion, pre110Codec); err != nil {
		return fmt.Errorf("couldn't register pre-1.1.0 codec: %w", err)
	}
	c := hierarchycodec.NewDefault()
	if err := vm.codec.RegisterCodec(currentCodecVersion, c); err != nil {
		return fmt.Errorf("couldn't register codec: %w", err)
	}
	if err := vm.genesisCodec.RegisterCodec(currentCodecVersion, c); err != nil {
		return fmt.Errorf("couldn't register codec: %w", err)
	}

	errs := wrappers.Errs{}
	errs.Add(
		vm.metrics.Initialize(ctx.Namespace, ctx.Metrics),

		vm.pubsub.Register("accepted"),
		vm.pubsub.Register("rejected"),
		vm.pubsub.Register("verified"),

		c.RegisterType(&BaseTx{}),
		c.RegisterType(&CreateAssetTx{}),
		c.RegisterType(&OperationTx{}),
		c.RegisterType(&ImportTx{}),
		c.RegisterType(&ExportTx{}),
		c.RegisterType(&CreateManagedAssetTx{}),
	)
	if errs.Errored() {
		return errs.Err
	}

	vm.fxs = make([]*parsedFx, len(fxs))
	for i, fxContainer := range fxs {
		if fxContainer == nil {
			return errIncompatibleFx
		}
		fx, ok := fxContainer.Fx.(Fx)
		if !ok {
			return errIncompatibleFx
		}
		vm.fxs[i] = &parsedFx{
			ID: fxContainer.ID,
			Fx: fx,
		}
		c.NextGroup()
		vm.codecRegistry = &codecRegistry{
			codecs:      []codec.Registry{c},
			index:       i,
			typeToIndex: vm.typeToFxIndex,
		}
		if err := fx.Initialize(vm); err != nil {
			return err
		}
	}

	vm.state = &prefixedState{
		state: &state{
			State: avax.State{
				Cache:               &cache.LRU{Size: stateCacheSize},
				DB:                  vm.db,
				GenesisCodec:        vm.genesisCodec,
				Codec:               vm.codec,
				CurrentCodecVersion: currentCodecVersion,
			},
		},
		tx:       &cache.LRU{Size: idCacheSize},
		utxo:     &cache.LRU{Size: idCacheSize},
		txStatus: &cache.LRU{Size: idCacheSize},
		uniqueTx: &cache.EvictableLRU{Size: txCacheSize},
	}

	if err := vm.initAliases(genesisBytes); err != nil {
		return err
	}

	if dbStatus, err := vm.state.DBInitialized(); err != nil || dbStatus == choices.Unknown {
		if err := vm.initState(genesisBytes); err != nil {
			return err
		}
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

	return vm.db.Commit()
}

// Returns the codec that was used before version 1.1.0
func pre110Codec(maxSliceLen int) (codec.Codec, error) {
	c := linearcodec.New(reflectcodec.DefaultTagName, maxSliceLen)
	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(&BaseTx{}),
		c.RegisterType(&CreateAssetTx{}),
		c.RegisterType(&OperationTx{}),
		c.RegisterType(&ImportTx{}),
		c.RegisterType(&ExportTx{}),
		c.RegisterType(&secp256k1fx.TransferInput{}),
		c.RegisterType(&secp256k1fx.MintOutput{}),
		c.RegisterType(&secp256k1fx.TransferOutput{}),
		c.RegisterType(&secp256k1fx.MintOperation{}),
		c.RegisterType(&secp256k1fx.Credential{}),
		c.RegisterType(&nftfx.MintOutput{}),
		c.RegisterType(&nftfx.TransferOutput{}),
		c.RegisterType(&nftfx.MintOperation{}),
		c.RegisterType(&nftfx.TransferOperation{}),
		c.RegisterType(&nftfx.Credential{}),
		c.RegisterType(&propertyfx.MintOutput{}),
		c.RegisterType(&propertyfx.OwnedOutput{}),
		c.RegisterType(&propertyfx.MintOperation{}),
		c.RegisterType(&propertyfx.BurnOperation{}),
		c.RegisterType(&propertyfx.Credential{}),
	)
	return c, errs.Err
}

// Bootstrapping is called by the consensus engine when it starts bootstrapping
// this chain
func (vm *VM) Bootstrapping() error {
	vm.metrics.numBootstrappingCalls.Inc()

	for _, fx := range vm.fxs {
		if err := fx.Fx.Bootstrapping(); err != nil {
			return err
		}
	}
	return nil
}

// Bootstrapped is called by the consensus engine when it is done bootstrapping
// this chain
func (vm *VM) Bootstrapped() error {
	vm.metrics.numBootstrappedCalls.Inc()

	for _, fx := range vm.fxs {
		if err := fx.Fx.Bootstrapped(); err != nil {
			return err
		}
	}
	vm.bootstrapped = true
	return nil
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

// CreateHandlers implements the avalanche.DAGVM interface
func (vm *VM) CreateHandlers() map[string]*common.HTTPHandler {
	vm.metrics.numCreateHandlersCalls.Inc()

	codec := cjson.NewCodec()

	rpcServer := rpc.NewServer()
	rpcServer.RegisterCodec(codec, "application/json")
	rpcServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	// name this service "avm"
	vm.ctx.Log.AssertNoError(rpcServer.RegisterService(&Service{vm: vm}, "avm"))

	walletServer := rpc.NewServer()
	walletServer.RegisterCodec(codec, "application/json")
	walletServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	// name this service "avm"
	vm.ctx.Log.AssertNoError(walletServer.RegisterService(&vm.walletService, "wallet"))

	return map[string]*common.HTTPHandler{
		"":        {Handler: rpcServer},
		"/wallet": {Handler: walletServer},
		"/pubsub": {LockOptions: common.NoLock, Handler: vm.pubsub},
	}
}

// CreateStaticHandlers implements the avalanche.DAGVM interface
func (vm *VM) CreateStaticHandlers() map[string]*common.HTTPHandler {
	newServer := rpc.NewServer()
	codec := cjson.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	// name this service "avm"
	staticService := CreateStaticService()
	_ = newServer.RegisterService(staticService, "avm")
	return map[string]*common.HTTPHandler{
		"": {LockOptions: common.WriteLock, Handler: newServer},
	}
}

// PendingTxs implements the avalanche.DAGVM interface
func (vm *VM) PendingTxs() []conflicts.Tx {
	vm.metrics.numPendingTxsCalls.Inc()

	vm.timer.Cancel()

	txs := vm.txs
	vm.txs = nil
	return txs
}

// ParseTx implements the avalanche.DAGVM interface
func (vm *VM) ParseTx(b []byte) (conflicts.Tx, error) {
	vm.metrics.numParseTxCalls.Inc()

	return vm.parseTx(b)
}

// GetTx implements the avalanche.DAGVM interface
func (vm *VM) GetTx(txID ids.ID) (conflicts.Tx, error) {
	vm.metrics.numGetTxCalls.Inc()

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

// GetAtomicUTXOs returns imported/exports UTXOs such that at least one of the addresses in [addrs] is referenced.
// Returns at most [limit] UTXOs.
// If [limit] <= 0 or [limit] > maxUTXOsToFetch, it is set to [maxUTXOsToFetch].
// Returns:
// * The fetched UTXOs
// * true if all there are no more UTXOs in this range to fetch
// * The address associated with the last UTXO fetched
// * The ID of the last UTXO fetched
func (vm *VM) GetAtomicUTXOs(
	chainID ids.ID,
	addrs ids.ShortSet,
	startAddr ids.ShortID,
	startUTXOID ids.ID,
	limit int,
) ([]*avax.UTXO, ids.ShortID, ids.ID, error) {
	if limit <= 0 || limit > maxUTXOsToFetch {
		limit = maxUTXOsToFetch
	}

	addrsList := make([][]byte, addrs.Len())
	i := 0
	for addr := range addrs {
		copied := addr
		addrsList[i] = copied[:]
		i++
	}

	allUTXOBytes, lastAddr, lastUTXO, err := vm.ctx.SharedMemory.Indexed(
		chainID,
		addrsList,
		startAddr.Bytes(),
		startUTXOID[:],
		limit,
	)
	if err != nil {
		return nil, ids.ShortID{}, ids.ID{}, fmt.Errorf("error fetching atomic UTXOs: %w", err)
	}

	lastAddrID, err := ids.ToShortID(lastAddr)
	if err != nil {
		lastAddrID = ids.ShortEmpty
	}
	lastUTXOID, err := ids.ToID(lastUTXO)
	if err != nil {
		lastUTXOID = ids.Empty
	}

	utxos := make([]*avax.UTXO, len(allUTXOBytes))
	for i, utxoBytes := range allUTXOBytes {
		utxo := &avax.UTXO{}
		if _, err := vm.codec.Unmarshal(utxoBytes, utxo); err != nil {
			return nil, ids.ShortID{}, ids.ID{}, fmt.Errorf("error parsing UTXO: %w", err)
		}
		utxos[i] = utxo
	}
	return utxos, lastAddrID, lastUTXOID, nil
}

// GetUTXOs returns UTXOs such that at least one of the addresses in [addrs] is referenced.
// Returns at most [limit] UTXOs.
// If [limit] <= 0 or [limit] > maxUTXOsToFetch, it is set to [maxUTXOsToFetch].
// Only returns UTXOs associated with addresses >= [startAddr].
// For address [startAddr], only returns UTXOs whose IDs are greater than [startUTXOID].
// Given a ![paginate] input all utxos will be fetched
// Returns:
// * The fetched UTXOs
// * The address associated with the last UTXO fetched
// * The ID of the last UTXO fetched
func (vm *VM) GetUTXOs(
	addrs ids.ShortSet,
	startAddr ids.ShortID,
	startUTXOID ids.ID,
	limit int,
	paginate bool,
) ([]*avax.UTXO, ids.ShortID, ids.ID, error) {
	if limit <= 0 || limit > maxUTXOsToFetch {
		limit = maxUTXOsToFetch
	}

	if paginate {
		return vm.getPaginatedUTXOs(addrs, startAddr, startUTXOID, limit)
	}
	return vm.getAllUTXOs(addrs)
}

func (vm *VM) getPaginatedUTXOs(addrs ids.ShortSet,
	startAddr ids.ShortID,
	startUTXOID ids.ID,
	limit int,
) ([]*avax.UTXO, ids.ShortID, ids.ID, error) {
	lastAddr := ids.ShortEmpty
	lastIndex := ids.Empty

	utxos := make([]*avax.UTXO, 0, limit)
	seen := make(ids.Set, limit) // IDs of UTXOs already in the list
	searchSize := limit          // the limit diminishes which can impact the expected return

	// enforces the same ordering for pagination
	addrsList := addrs.List()
	ids.SortShortIDs(addrsList)

	for _, addr := range addrsList {
		start := ids.Empty
		if comp := bytes.Compare(addr.Bytes(), startAddr.Bytes()); comp == -1 { // Skip addresses before [startAddr]
			continue
		} else if comp == 0 {
			start = startUTXOID
		}

		// Get UTXOs associated with [addr]. [searchSize] is used here to ensure
		// that no UTXOs are dropped due to duplicated fetching.
		utxoIDs, err := vm.state.Funds(addr.Bytes(), start, searchSize)
		if err != nil {
			return nil, ids.ShortID{}, ids.ID{}, fmt.Errorf("couldn't get UTXOs for address %s: %w", addr, err)
		}
		for _, utxoID := range utxoIDs {
			lastIndex = utxoID // The last searched UTXO - not the last found
			lastAddr = addr    // The last address searched that has UTXOs (even duplicated) - not the last found

			if seen.Contains(utxoID) { // Already have this UTXO in the list
				continue
			}

			utxo, err := vm.state.UTXO(utxoID)
			if err != nil {
				return nil, ids.ShortID{}, ids.ID{}, fmt.Errorf("couldn't get UTXO %s: %w", utxoID, err)
			}

			utxos = append(utxos, utxo)
			seen.Add(utxoID)
			limit--
			if limit <= 0 {
				return utxos, lastAddr, lastIndex, nil // Found [limit] utxos; stop.
			}
		}
	}
	return utxos, lastAddr, lastIndex, nil // Didnt reach the [limit] utxos; no more were found
}

func (vm *VM) getAllUTXOs(addrs ids.ShortSet) ([]*avax.UTXO, ids.ShortID, ids.ID, error) {
	var err error
	lastAddr := ids.ShortEmpty
	lastIndex := ids.Empty
	seen := make(ids.Set, maxUTXOsToFetch) // IDs of UTXOs already in the list
	utxos := make([]*avax.UTXO, 0, maxUTXOsToFetch)

	// enforces the same ordering for pagination
	addrsList := addrs.List()
	ids.SortShortIDs(addrsList)

	// iterate over the addresses and get all the utxos
	for _, addr := range addrsList {
		lastIndex, err = vm.getAllUniqueAddressUTXOs(addr, &seen, &utxos)
		if err != nil {
			return nil, ids.ShortID{}, ids.ID{}, fmt.Errorf("couldn't get UTXOs for address %s: %w", addr, err)
		}

		if lastIndex != ids.Empty {
			lastAddr = addr // The last address searched that has UTXOs (even duplicated) - not the last found
		}
	}
	return utxos, lastAddr, lastIndex, nil
}

func (vm *VM) getAllUniqueAddressUTXOs(addr ids.ShortID, seen *ids.Set, utxos *[]*avax.UTXO) (ids.ID, error) {
	lastIndex := ids.Empty

	for {
		utxoIDs, err := vm.state.Funds(addr.Bytes(), lastIndex, maxUTXOsToFetch) // Get UTXOs associated with [addr]
		if err != nil {
			return ids.ID{}, err
		}

		if len(utxoIDs) == 0 {
			return lastIndex, nil
		}

		for _, utxoID := range utxoIDs {
			lastIndex = utxoID // The last searched UTXO - not the last found

			if seen.Contains(utxoID) { // Already have this UTXO in the list
				continue
			}

			utxo, err := vm.state.UTXO(utxoID)
			if err != nil {
				return ids.ID{}, err
			}
			*utxos = append(*utxos, utxo)
			seen.Add(utxoID)
		}
	}
}

/*
 ******************************************************************************
 *********************************** Fx API ***********************************
 ******************************************************************************
 */

// Clock returns a reference to the internal clock of this VM
func (vm *VM) Clock() *timer.Clock { return &vm.clock }

// Codec returns a reference to the internal codec of this VM
func (vm *VM) Codec() codec.Manager { return vm.codec }

// CodecRegistry returns a reference to the internal codec registry of this VM
func (vm *VM) CodecRegistry() codec.Registry { return vm.codecRegistry }

// Logger returns a reference to the internal logger of this VM
func (vm *VM) Logger() logging.Logger { return vm.ctx.Log }

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
			vm.ctx.Log.Warn("Delaying issuance of transactions due to contention")
			vm.timer.SetTimeoutIn(vm.batchTimeout)
		}
	}
}

/*
 ******************************************************************************
 ********************************** Helpers ***********************************
 ******************************************************************************
 */

func (vm *VM) initAliases(genesisBytes []byte) error {
	genesis := Genesis{}
	if version, err := vm.genesisCodec.Unmarshal(genesisBytes, &genesis); err != nil {
		return err
	} else if version != pre110CodecVersion {
		return fmt.Errorf("expected codec version %d but got %d", pre110CodecVersion, version)
	}

	for _, genesisTx := range genesis.Txs {
		if len(genesisTx.Outs) != 0 {
			return errGenesisAssetMustHaveState
		}

		tx := Tx{
			UnsignedTx: &genesisTx.CreateAssetTx,
		}

		// Use the original codec so that the derived transaction ID doesn't change
		if err := tx.SignSECP256K1Fx(vm.genesisCodec, pre110CodecVersion, nil); err != nil {
			return err
		}

		txID := tx.ID()
		if err := vm.Alias(txID, genesisTx.Alias); err != nil {
			return err
		}
	}

	return nil
}

func (vm *VM) initState(genesisBytes []byte) error {
	genesis := Genesis{}
	if version, err := vm.genesisCodec.Unmarshal(genesisBytes, &genesis); err != nil {
		return err
	} else if version != pre110CodecVersion {
		return fmt.Errorf("expected codec version %d but got %d", pre110CodecVersion, version)
	}

	for _, genesisTx := range genesis.Txs {
		if len(genesisTx.Outs) != 0 {
			return errGenesisAssetMustHaveState
		}

		tx := Tx{
			UnsignedTx: &genesisTx.CreateAssetTx,
		}
		if err := tx.SignSECP256K1Fx(vm.genesisCodec, pre110CodecVersion, nil); err != nil {
			return err
		}

		txID := tx.ID()
		vm.ctx.Log.Info("initializing with AssetID %s", txID)
		if err := vm.state.SetTx(txID, &tx); err != nil {
			return err
		}
		if err := vm.state.SetStatus(txID, choices.Accepted); err != nil {
			return err
		}
		for _, utxo := range tx.UTXOs() {
			if err := vm.state.FundUTXO(utxo); err != nil {
				return err
			}
			if out, ok := utxo.Out.(*secp256k1fx.ManagedAssetStatusOutput); ok {
				if err := vm.state.PutManagedAssetStatus(utxo.AssetID(), tx.Epoch(), out.Frozen, &out.Manager); err != nil {
					return fmt.Errorf("couldn't freeze asset: %w", err)
				}
			}
		}
	}

	return vm.state.SetDBInitialized(choices.Processing)
}

func (vm *VM) parseTx(bytes []byte) (*UniqueTx, error) {
	rawTx, err := vm.parsePrivateTx(bytes)
	if err != nil {
		return nil, err
	}

	tx := &UniqueTx{
		TxState: &TxState{
			Tx: rawTx,
		},
		vm:   vm,
		txID: rawTx.ID(),
	}
	if err := tx.SyntacticVerify(); err != nil {
		return nil, err
	}

	if tx.Status() == choices.Unknown {
		if err := vm.state.SetTx(tx.ID(), tx.Tx); err != nil {
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
	codecVersion, err := vm.codec.Unmarshal(txBytes, tx)
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

func (vm *VM) issueTx(tx conflicts.Tx) {
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
	utxo, err := vm.state.UTXO(inputID)
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

	var createAssetTx *CreateAssetTx
	switch unsignedTx := tx.UnsignedTx.(type) {
	case *CreateAssetTx:
		createAssetTx = unsignedTx
	case *CreateManagedAssetTx:
		createAssetTx = &unsignedTx.CreateAssetTx
	default:
		// This transaction was not an asset creation tx
		// (Neither regular nor managed)
		return false
	}

	fxIDs := ids.BitSet(0)
	for _, state := range createAssetTx.States {
		if state.FxID == uint32(fxID) {
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
	if utxoAssetID != in.AssetID() {
		return errAssetIDMismatch
	}

	if !vm.verifyFxUsage(fxIndex, utxoAssetID) {
		return errIncompatibleFx
	}

	// Check if the UTXO's asset if frozen
	// TODO If the asset is frozen, compare the epoch during which
	// it was locked to the current one. Similar for unfreeze.
	_, frozen, _, err := vm.state.ManagedAssetStatus(utxoAssetID)
	if err != nil && err != database.ErrNotFound {
		// Couldn't get asset status from database
		return err
	}
	if err == nil && frozen { // TODO check epoch here
		// Got asset status from database and it's frozen
		return fmt.Errorf("asset %s is frozen", utxoAssetID)
	}

	if err := fx.VerifyTransfer(in.In, utxo.Out); err != nil {
		return err
	}

	// See if credential [cred] gives permission to spend the UTXO
	// based on the UTXO's output
	err = fx.VerifyPermission(tx, in.In, cred, utxo.Out)
	if err == nil {
		return nil
	}

	// Check whether [assetID] is a managed asset, and if so, who its manager is
	_, _, manager, err := vm.state.ManagedAssetStatus(utxoAssetID)
	if err != nil {
		if err != database.ErrNotFound {
			return fmt.Errorf("couldn't get asset status: %w", err)
		}
		return errNoPermission
	}

	// Check whether [cred] was signed by [manager]
	if err := fx.VerifyPermission(tx, in.In, cred, manager); err != nil {
		return errNoPermission // It wasn't
	}
	return nil
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
		if utxo.AssetID() != opAssetID {
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

	if _, ok := op.Op.(*secp256k1fx.UpdateManagedAssetOperation); ok {
		// This operation updates a managed asset's status
		// Make sure the last time the status was updated was more than 1 epoch before [tx]
		// i.e. if [tx] is in epoch n, the latest possible epoch in which the status
		// may have been updated is [n-2] .
		// Get the epoch in which the asset's status was most recently updated
		epoch, _, _, err := vm.state.ManagedAssetStatus(opAssetID)
		if err != nil {
			return fmt.Errorf("couldn't get managed asset's status: %w", err)
		}
		if tx.Epoch() < epoch { // TODO change this to tx.Epoch() < epoch+2
			return fmt.Errorf(
				"asset update epoch (%d) must be >= 2 + most recent status update epoch (%d)",
				tx.Epoch(),
				epoch,
			)
		}
	}

	// TODO check if this operation is trying to do a freeze with an invalid AssetManagerOutput
	// that hasn't taken effect yet
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
	db, err := vm.ctx.Keystore.GetDatabase(username, password)
	if err != nil {
		return nil, nil, fmt.Errorf("problem retrieving user: %w", err)
	}
	// Drop any potential error closing the database to report the original
	// error
	defer db.Close()

	user := userState{vm: vm}

	kc, err := user.Keychain(db, addrsToUse)
	if err != nil {
		return nil, nil, err
	}

	utxos, _, _, err := vm.GetUTXOs(kc.Addresses(), ids.ShortEmpty, ids.Empty, -1, false)
	if err != nil {
		return nil, nil, fmt.Errorf("problem retrieving user's UTXOs: %w", err)
	}

	return utxos, kc, db.Close()
}

// Spend ...
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

// SpendNFT ...
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

// SpendAll ...
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

// Mint ...
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

// UpdateManagedAssetStatus attempts to use the given UTXOs and keychain
// to create an NewUpdateManagedAssetStatusOperation that updates the
// status of asset [assetID].
// The UTXOs can't be consumed if they are locked at [time]
func newUpdateManagedAssetStatusOperation(
	utxos []*avax.UTXO,
	kc *secp256k1fx.Keychain,
	assetID ids.ID,
	time uint64,
	frozen bool,
	manager *secp256k1fx.OutputOwners,
) (
	*Operation,
	[]*crypto.PrivateKeySECP256K1R,
	error,
) {
	for _, utxo := range utxos {
		// This UTXO isn't the right asset ID
		if assetID != utxo.AssetID() {
			continue
		}

		// Need to consume a *ManagedAssetStatusOutput for this operation
		out, ok := utxo.Out.(*secp256k1fx.ManagedAssetStatusOutput)
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
		op := &Operation{
			Asset:   avax.Asset{ID: assetID},
			UTXOIDs: []*avax.UTXOID{&utxo.UTXOID},
			Op: &secp256k1fx.UpdateManagedAssetOperation{
				Input: *in,
				ManagedAssetStatusOutput: secp256k1fx.ManagedAssetStatusOutput{
					Frozen:  frozen,
					Manager: *manager,
				},
			},
		}
		return op, signers, nil
	}
	return nil, nil, fmt.Errorf("the given UTXOs/keys can't update asset %s", assetID)
}

// mintManagedAsset attempts to use the given UTXOs and keychain
// to create an NewUpdateManagedAssetStatusOperation that mints
// [mintAmt] units of the asset. The minted units are spendable
// with [mintRecipientThreshold] signatures from [mintRecipients].
// The UTXOs can't be consumed if they are locked at [time].
func mintManagedAsset(
	utxos []*avax.UTXO,
	kc *secp256k1fx.Keychain,
	assetID ids.ID,
	time uint64,
	mintAmt uint64,
	mintRecipients []ids.ShortID,
	mintRecipientsThreshold uint32,
) (
	*Operation,
	[]*crypto.PrivateKeySECP256K1R,
	error,
) {
	for _, utxo := range utxos {
		// This UTXO isn't the right asset ID
		if assetID != utxo.AssetID() {
			continue
		}

		// Need to consume a *ManagedAssetStatusOutput for this operation
		out, ok := utxo.Out.(*secp256k1fx.ManagedAssetStatusOutput)
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
		op := &Operation{
			Asset:   avax.Asset{ID: assetID},
			UTXOIDs: []*avax.UTXOID{&utxo.UTXOID},
			Op: &secp256k1fx.UpdateManagedAssetOperation{
				Input: *in,
				ManagedAssetStatusOutput: secp256k1fx.ManagedAssetStatusOutput{
					Frozen:  out.Frozen,
					Manager: out.Manager,
				},
				Mint: true,
				TransferOutput: secp256k1fx.TransferOutput{
					Amt: mintAmt,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: mintRecipientsThreshold,
						Addrs:     mintRecipients,
					},
				},
			},
		}
		return op, signers, nil
	}
	return nil, nil, fmt.Errorf("the given UTXOs/keys can't mint asset %s", assetID)
}

// MintNFT ...
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

// ParseLocalAddress takes in an address for this chain and produces the ID
func (vm *VM) ParseLocalAddress(addrStr string) (ids.ShortID, error) {
	chainID, addr, err := vm.ParseAddress(addrStr)
	if err != nil {
		return ids.ShortID{}, err
	}
	if chainID != vm.ctx.ChainID {
		return ids.ShortID{}, fmt.Errorf("expected chainID to be %q but was %q",
			vm.ctx.ChainID, chainID)
	}
	return addr, nil
}

// ParseAddress takes in an address and produces the ID of the chain it's for
// the ID of the address
func (vm *VM) ParseAddress(addrStr string) (ids.ID, ids.ShortID, error) {
	chainIDAlias, hrp, addrBytes, err := formatting.ParseAddress(addrStr)
	if err != nil {
		return ids.ID{}, ids.ShortID{}, err
	}

	chainID, err := vm.ctx.BCLookup.Lookup(chainIDAlias)
	if err != nil {
		return ids.ID{}, ids.ShortID{}, err
	}

	expectedHRP := constants.GetHRP(vm.ctx.NetworkID)
	if hrp != expectedHRP {
		return ids.ID{}, ids.ShortID{}, fmt.Errorf("expected hrp %q but got %q",
			expectedHRP, hrp)
	}

	addr, err := ids.ToShortID(addrBytes)
	if err != nil {
		return ids.ID{}, ids.ShortID{}, err
	}
	return chainID, addr, nil
}

// FormatLocalAddress takes in a raw address and produces the formatted address
func (vm *VM) FormatLocalAddress(addr ids.ShortID) (string, error) {
	return vm.FormatAddress(vm.ctx.ChainID, addr)
}

// FormatAddress takes in a chainID and a raw address and produces the formatted
// address
func (vm *VM) FormatAddress(chainID ids.ID, addr ids.ShortID) (string, error) {
	chainIDAlias, err := vm.ctx.BCLookup.PrimaryAlias(chainID)
	if err != nil {
		return "", err
	}
	hrp := constants.GetHRP(vm.ctx.NetworkID)
	return formatting.FormatAddress(chainIDAlias, hrp, addr.Bytes())
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
