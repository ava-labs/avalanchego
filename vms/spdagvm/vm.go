// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spdagvm

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/gorilla/rpc/v2"

	"github.com/ava-labs/gecko/cache"
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowstorm"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/math"
	"github.com/ava-labs/gecko/utils/timer"

	jsoncodec "github.com/ava-labs/gecko/utils/json"
)

const (
	batchTimeout   = time.Second
	batchSize      = 30
	stateCacheSize = 10000
	idCacheSize    = 10000
	txCacheSize    = 10000
)

var (
	errUnknownUTXOType = errors.New("utxo has unknown output type")
	errAsset           = errors.New("assetID must be blank")
	errAmountOverflow  = errors.New("the amount of this transaction plus the transaction fee overflows")
	errUnsupportedFXs  = errors.New("unsupported feature extensions")
)

// VM implements the avalanche.DAGVM interface
type VM struct {
	// The context of this vm
	ctx *snow.Context

	// Used to check local time
	clock timer.Clock

	// State management
	state *prefixedState

	// Transaction issuing
	timer *timer.Timer

	// Transactions will be sent to consensus after at most [batchTimeout]
	batchTimeout time.Duration

	// Transactions that have not yet been sent to consensus
	txs []snowstorm.Tx

	// Channel through which the vm notifies the consensus engine
	// that there are transactions to add to consensus
	toEngine chan<- common.Message

	// The transaction fee, which the sender pays. The fee is burned.
	TxFee uint64

	baseDB database.Database
	db     *versiondb.Database
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
	if len(fxs) != 0 {
		return errUnsupportedFXs
	}
	vm.ctx = ctx
	vm.baseDB = db
	vm.db = versiondb.New(db)
	vm.state = &prefixedState{
		state: &state{
			c:  &cache.LRU{Size: stateCacheSize},
			vm: vm,
		},

		tx:       &cache.LRU{Size: idCacheSize},
		utxo:     &cache.LRU{Size: idCacheSize},
		txStatus: &cache.LRU{Size: idCacheSize},
		funds:    &cache.LRU{Size: idCacheSize},

		uniqueTx: &cache.EvictableLRU{Size: txCacheSize},
	}

	// Initialize the database if it has not already been initialized
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
	go vm.ctx.Log.RecoverAndPanic(vm.timer.Dispatch)
	vm.batchTimeout = batchTimeout
	vm.toEngine = toEngine

	return vm.db.Commit()
}

// Bootstrapping marks this VM as bootstrapping
func (vm *VM) Bootstrapping() error { return nil }

// Bootstrapped marks this VM as bootstrapped
func (vm *VM) Bootstrapped() error { return nil }

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

// CreateHandlers makes new service objects with references to the vm
func (vm *VM) CreateHandlers() map[string]*common.HTTPHandler {
	newServer := rpc.NewServer()
	codec := jsoncodec.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	// name this service "spdag"
	vm.ctx.Log.AssertNoError(newServer.RegisterService(&Service{vm: vm}, "spdag"))
	return map[string]*common.HTTPHandler{
		"": {Handler: newServer},
	}
}

// CreateStaticHandlers makes new service objects without references to the vm
func (vm *VM) CreateStaticHandlers() map[string]*common.HTTPHandler {
	newServer := rpc.NewServer()
	codec := jsoncodec.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	// name this service "spdag"
	_ = newServer.RegisterService(&StaticService{}, "spdag")
	return map[string]*common.HTTPHandler{
		// NoLock because the static functions probably wont be stateful (i.e. no
		// write operations)
		"": {LockOptions: common.NoLock, Handler: newServer},
	}
}

// PendingTxs returns the transactions that have not yet
// been added to consensus
func (vm *VM) PendingTxs() []snowstorm.Tx {
	txs := vm.txs

	vm.txs = nil
	vm.timer.Cancel()

	return txs
}

// ParseTx parses bytes to a *UniqueTx
func (vm *VM) ParseTx(b []byte) (snowstorm.Tx, error) { return vm.parseTx(b, nil) }

// GetTx returns the transaction whose ID is [txID]
func (vm *VM) GetTx(txID ids.ID) (snowstorm.Tx, error) {
	rawTx, err := vm.state.Tx(txID)
	if err != nil {
		return nil, err
	}

	tx := &UniqueTx{
		vm:   vm,
		txID: rawTx.ID(),
		t: &txState{
			tx: rawTx,
		},
	}
	// Verify must be called in the case the that tx was flushed from the unique
	// cache.
	if err := tx.VerifyState(); err != nil {
		vm.ctx.Log.Debug("GetTx resulted in fetching a tx that failed verification: %s", err)
		if err := tx.setStatus(choices.Rejected); err != nil {
			return nil, err
		}
	}

	return tx, nil
}

/*
 ******************************************************************************
 ******************************** Wallet API **********************************
 ******************************************************************************
 */

// CreateKey returns a new base58-encoded private key
func (vm *VM) CreateKey() (string, error) {
	factory := crypto.FactorySECP256K1R{}
	pk, err := factory.NewPrivateKey()
	if err != nil {
		return "", err
	}
	cb58 := formatting.CB58{Bytes: pk.Bytes()}
	return cb58.String(), nil
}

// GetAddress returns the string repr. of the address
// controlled by a base58-encoded private key
func (vm *VM) GetAddress(privKeyStr string) (string, error) {
	cb58 := formatting.CB58{}
	if err := cb58.FromString(privKeyStr); err != nil {
		return "", err
	}
	factory := crypto.FactorySECP256K1R{}
	pk, err := factory.ToPrivateKey(cb58.Bytes)
	if err != nil {
		return "", err
	}
	return pk.PublicKey().Address().String(), nil
}

// GetBalance returns [address]'s balance of the asset whose
// ID is [assetID]
func (vm *VM) GetBalance(address, assetID string) (uint64, error) {
	if assetID != "" {
		return 0, errAsset
	}

	// Parse the string repr. of the address to an ids.ShortID
	addr, err := ids.ShortFromString(address)
	if err != nil {
		return 0, err
	}

	addrSet := ids.ShortSet{addr.Key(): true} // Note this set contains only [addr]
	utxos, err := vm.GetUTXOs(addrSet)        // The UTXOs that reference [addr]
	if err != nil {
		return 0, err
	}

	// Go through each UTXO that references [addr].
	// If the private key that controls [addr] may spend the UTXO,
	// add its amount to [balance]
	balance := uint64(0)
	currentTime := vm.clock.Unix()
	for _, utxo := range utxos {
		switch out := utxo.Out().(type) {
		case *OutputPayment:
			// Because [addrSet] has size 1, we know [addr] is
			// referenced in [out]
			if currentTime > out.Locktime() && out.Threshold() == 1 {
				amount, err := math.Add64(balance, out.Amount())
				if err != nil {
					return 0, err
				}
				balance = amount // This is not a mistake. It should _not_ be +=. The adding is done by math.Add64 a few lines above.
			}
		case *OutputTakeOrLeave:
			addresses := ids.ShortSet{}
			addresses.Add(out.Addresses1()...)
			if addresses.Contains(addr) && currentTime > out.Locktime1() && out.Threshold1() == 1 {
				amount, err := math.Add64(balance, out.Amount())
				if err != nil {
					return 0, err
				}
				balance = amount
			}

			addresses.Clear()
			addresses.Add(out.Addresses2()...)
			if addresses.Contains(addr) && currentTime > out.Locktime2() && out.Threshold2() == 1 {
				amount, err := math.Add64(balance, out.Amount())
				if err != nil {
					return 0, err
				}
				balance = amount
			}
		default: // TODO: Should this error? Or should we just ignore outputs we don't recognize?
			return 0, errUnknownOutputType
		}
	}
	return balance, nil
}

// ListAssets returns the IDs of assets such that [address] has
// a non-zero balance of that asset
func (vm *VM) ListAssets(address string) ([]string, error) {
	balance, err := vm.GetBalance(address, "")
	if balance > 0 && err == nil {
		return []string{""}, nil
	}
	return []string{}, err
}

// Send issues a transaction that sends [amount] from the addresses controlled
// by [fromPKs] to [toAddrStr]. Send returns the transaction's ID. Any "change"
// will be sent to the address controlled by the first element of [fromPKs].
func (vm *VM) Send(amount uint64, assetID, toAddrStr string, fromPKs []string) (string, error) {
	// The assetID must be empty
	if assetID != "" {
		return "", errAsset
	}

	// Add all of the keys in [fromPKs] to a keychain
	keychain := NewKeychain(vm.ctx.NetworkID, vm.ctx.ChainID)
	factory := crypto.FactorySECP256K1R{}
	cb58 := formatting.CB58{}
	for _, fpk := range fromPKs {
		// Parse the string repr. of the private key to bytes
		if err := cb58.FromString(fpk); err != nil {
			return "", err
		}
		// Parse the byte repr. to a crypto.PrivateKey
		pk, err := factory.ToPrivateKey(cb58.Bytes)
		if err != nil {
			return "", err
		}
		// Parse the crpyo.PrivateKey repr. to a crypto.PrivateKeySECP256K1R
		keychain.Add(pk.(*crypto.PrivateKeySECP256K1R))
	}

	// Parse [toAddrStr] to an ids.ShortID
	toAddr, err := ids.ShortFromString(toAddrStr)
	if err != nil {
		return "", err
	}
	toAddrs := []ids.ShortID{toAddr}
	outAddrStr, err := vm.GetAddress(fromPKs[0])
	if err != nil {
		return "", err
	}
	outAddr, err := ids.ShortFromString(outAddrStr)
	if err != nil {
		return "", err
	}

	// Get the UTXOs controlled by the keys in [fromPKs]
	utxos, err := vm.GetUTXOs(keychain.Addresses())
	if err != nil {
		return "", err
	}

	// Build the transaction
	builder := Builder{
		NetworkID: vm.ctx.NetworkID,
		ChainID:   vm.ctx.ChainID,
	}
	currentTime := vm.clock.Unix()
	tx, err := builder.NewTxFromUTXOs(keychain, utxos, amount, vm.TxFee, 0, 1, toAddrs, outAddr, currentTime)
	if err != nil {
		return "", err
	}

	// Wrap the *Tx to make it a snowstorm.Tx
	wrappedTx, err := vm.wrapTx(tx, nil)
	if err != nil {
		return "", err
	}

	// Issue the transaction
	vm.issueTx(wrappedTx)
	return tx.ID().String(), nil
}

// GetTxHistory takes an address and returns an ordered list of known records containing
// key-value pairs of data.
func (vm *VM) GetTxHistory(address string) (string, []string, map[string]string, []map[string]string, error) {
	addr, err := ids.ShortFromString(address)
	if err != nil {
		return "", nil, nil, nil, err
	}
	addrSet := ids.ShortSet{addr.Key(): true}
	utxos, err := vm.GetUTXOs(addrSet)
	if err != nil {
		return "", nil, nil, nil, err
	}

	result := []map[string]string{}
	for _, utxo := range utxos {
		r := map[string]string{
			"TxID":    utxo.sourceID.String(),
			"TxIndex": fmt.Sprint(utxo.sourceIndex),
		}
		switch v := utxo.out.(type) {
		case *OutputPayment:
			r["Amount"] = strconv.FormatUint(v.Amount(), 10)
			r["Locktime"] = strconv.FormatUint(v.Locktime(), 10)
		case *OutputTakeOrLeave:
			r["Amount"] = strconv.FormatUint(v.Amount(), 10)
			r["Locktime"] = strconv.FormatUint(v.Locktime1(), 10)
		default:
			return "", nil, nil, nil, errUnknownUTXOType
		}
		result = append(result, r)
	}
	title := "UTXO Data"
	fieldKeys := []string{"TxID", "TxIndex", "Amount", "Locktime"}
	fieldNames := map[string]string{
		"TxID":     "TxID",
		"TxIndex":  "TxIndex",
		"Amount":   "Amount",
		"Locktime": "Locktime",
	}
	return title, fieldKeys, fieldNames, result, nil
}

// GetUTXOs returns the UTXOs such that at least one address in [addrs] is
// referenced in the UTXO.
func (vm *VM) GetUTXOs(addrs ids.ShortSet) ([]*UTXO, error) {
	utxoIDs := ids.Set{}
	for _, addr := range addrs.List() {
		if utxos, err := vm.state.Funds(addr.LongID()); err == nil {
			utxoIDs.Add(utxos...)
		}
	}

	utxos := []*UTXO{}
	for _, utxoID := range utxoIDs.List() {
		utxo, err := vm.state.UTXO(utxoID)
		if err != nil {
			return nil, err
		}
		utxos = append(utxos, utxo)
	}
	return utxos, nil
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
			vm.ctx.Log.Warn("Delaying issuance of transactions due to contention")
			vm.timer.SetTimeoutIn(vm.batchTimeout)
		}
	}
}

/*
 ******************************************************************************
 ******************************* Deprecated API *******************************
 ******************************************************************************
 */

// IssueTx implements the avalanche.DAGVM interface
func (vm *VM) IssueTx(b []byte, finalized func(choices.Status)) (ids.ID, error) {
	tx, err := vm.parseTx(b, finalized)
	if err != nil {
		return ids.ID{}, err
	}
	if err := tx.Verify(); err != nil {
		return ids.ID{}, err
	}
	vm.issueTx(tx)
	return tx.ID(), nil
}

/*
 ******************************************************************************
 ******************************* Implementation *******************************
 ******************************************************************************
 */

// Initialize state using [genesisBytes] as the genesis data
func (vm *VM) initState(genesisBytes []byte) error {
	c := Codec{}
	tx, err := c.UnmarshalTx(genesisBytes)
	if err != nil {
		return err
	}
	if err := vm.state.SetTx(tx.ID(), tx); err != nil {
		return err
	}
	if err := vm.state.SetStatus(tx.ID(), choices.Accepted); err != nil {
		return err
	}
	for _, utxo := range tx.UTXOs() {
		if err := vm.state.FundUTXO(utxo); err != nil {
			return err
		}
	}

	return vm.state.SetDBInitialized(choices.Processing)
}

// TODO: Remove the callback from this function
func (vm *VM) parseTx(b []byte, finalized func(choices.Status)) (*UniqueTx, error) {
	c := Codec{}
	rawTx, err := c.UnmarshalTx(b)
	if err != nil {
		return nil, err
	}
	return vm.wrapTx(rawTx, finalized)
}

// TODO: Remove the callback from this function
func (vm *VM) wrapTx(rawTx *Tx, finalized func(choices.Status)) (*UniqueTx, error) {
	tx := &UniqueTx{
		vm:   vm,
		txID: rawTx.ID(),
		t: &txState{
			tx: rawTx,
		},
	}
	if err := tx.VerifyTx(); err != nil {
		return nil, err
	}

	if tx.Status() == choices.Unknown {
		if err := vm.state.SetTx(tx.ID(), tx.t.tx); err != nil {
			return nil, err
		}
		if err := tx.setStatus(choices.Processing); err != nil {
			return nil, err
		}
	}

	tx.addEvents(finalized)
	return tx, nil
}

func (vm *VM) issueTx(tx snowstorm.Tx) {
	vm.txs = append(vm.txs, tx)
	switch {
	// Flush the transactions if enough transactions are waiting
	case len(vm.txs) == batchSize:
		vm.FlushTxs()
	// Set timeout so we flush this transaction after at most [p.batchTimeout]
	case len(vm.txs) == 1:
		vm.timer.SetTimeoutIn(vm.batchTimeout)
	}
}
