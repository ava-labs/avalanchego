// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spchainvm

import (
	"errors"
	"time"

	"github.com/gorilla/rpc/v2"

	"github.com/ava-labs/gecko/cache"
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowman"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/timer"
	"github.com/ava-labs/gecko/utils/wrappers"

	jsoncodec "github.com/ava-labs/gecko/utils/json"
)

const (
	batchTimeout = time.Second
	idCacheSize  = 10000
	sigCache     = 10000
)

var (
	maxBatchSize = 30
)

var (
	errNoTxs          = errors.New("no transactions")
	errUnsupportedFXs = errors.New("unsupported feature extensions")
)

// VM implements the snowman.ChainVM interface
type VM struct {
	ctx *snow.Context

	// State management
	state  *prefixedState
	baseDB database.Database

	factory crypto.FactorySECP256K1R

	// The ID of the last accepted block
	lastAccepted ids.ID

	// Transaction issuing
	preferred ids.ID
	toEngine  chan<- common.Message
	timer     *timer.Timer
	txs       []*Tx

	currentBlocks map[[32]byte]*LiveBlock

	onAccept func(ids.ID)
}

/*
 ******************************************************************************
 ********************************* Snowman API ********************************
 ******************************************************************************
 */

// Initialize implements the snowman.ChainVM interface
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
	vm.state = &prefixedState{
		block:   &cache.LRU{Size: idCacheSize},
		account: &cache.LRU{Size: idCacheSize},
		status:  &cache.LRU{Size: idCacheSize},
	}
	vm.baseDB = db
	vm.factory.Cache.Size = sigCache

	if dbStatus, err := vm.state.DBInitialized(db); err != nil || dbStatus == choices.Unknown {
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
	vm.toEngine = toEngine

	lastAccepted, err := vm.state.LastAccepted(db)
	if err != nil {
		return err
	}
	vm.lastAccepted = lastAccepted

	vm.currentBlocks = make(map[[32]byte]*LiveBlock)
	return nil
}

// Bootstrapping marks this VM as bootstrapping
func (vm *VM) Bootstrapping() error { return nil }

// Bootstrapped marks this VM as bootstrapped
func (vm *VM) Bootstrapped() error { return nil }

// Shutdown implements the snowman.ChainVM interface
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

// BuildBlock implements the snowman.ChainVM interface
func (vm *VM) BuildBlock() (snowman.Block, error) {
	vm.timer.Cancel()

	if len(vm.txs) == 0 {
		return nil, errNoTxs
	}

	defer vm.FlushTxs()

	txs := vm.txs
	if len(txs) > maxBatchSize {
		txs = txs[:maxBatchSize]
	}
	vm.txs = vm.txs[len(txs):]

	builder := Builder{
		NetworkID: 0,
		ChainID:   vm.ctx.ChainID,
	}
	rawBlock, err := builder.NewBlock(vm.preferred, txs)
	if err != nil {
		vm.ctx.Log.Warn("Dropping transactions due to %s", err)
		return nil, err
	}
	rawBlock.startVerify(vm.ctx, &vm.factory)
	block := &LiveBlock{
		vm:    vm,
		block: rawBlock,
	}
	return block, vm.state.SetBlock(vm.baseDB, rawBlock.ID(), rawBlock)
}

// ParseBlock implements the snowman.ChainVM interface
func (vm *VM) ParseBlock(b []byte) (snowman.Block, error) {
	c := Codec{}
	rawBlock, err := c.UnmarshalBlock(b)
	if err != nil {
		return nil, err
	}
	rawBlock.startVerify(vm.ctx, &vm.factory)
	block := &LiveBlock{
		vm:    vm,
		block: rawBlock,
	}
	return block, vm.state.SetBlock(vm.baseDB, rawBlock.ID(), rawBlock)
}

// SaveBlock implements the snowman.ChainVM interface
func (vm *VM) SaveBlock(blk snowman.Block) error {
	_, err := vm.ParseBlock(blk.Bytes())
	return err
}

// GetBlock implements the snowman.ChainVM interface
func (vm *VM) GetBlock(id ids.ID) (snowman.Block, error) {
	blk, err := vm.state.Block(vm.baseDB, id)
	if err != nil {
		return nil, err
	}
	blk.startVerify(vm.ctx, &vm.factory)
	return &LiveBlock{
		vm:    vm,
		block: blk,
	}, nil
}

// SetPreference sets what the current tail of the chain is
func (vm *VM) SetPreference(preferred ids.ID) { vm.preferred = preferred }

// LastAccepted returns the last accepted block ID
func (vm *VM) LastAccepted() ids.ID { return vm.lastAccepted }

// CreateHandlers makes new service objects with references to the vm
func (vm *VM) CreateHandlers() map[string]*common.HTTPHandler {
	newServer := rpc.NewServer()
	codec := jsoncodec.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	// Name the API service "spchain"
	vm.ctx.Log.AssertNoError(newServer.RegisterService(&Service{vm: vm}, "spchain"))
	return map[string]*common.HTTPHandler{
		"": &common.HTTPHandler{LockOptions: common.WriteLock, Handler: newServer},
	}
}

// CreateStaticHandlers makes new service objects without references to the vm
func (vm *VM) CreateStaticHandlers() map[string]*common.HTTPHandler {
	newServer := rpc.NewServer()
	codec := jsoncodec.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	// Name the API service "spchain"
	_ = newServer.RegisterService(&StaticService{}, "spchain")
	return map[string]*common.HTTPHandler{
		"": &common.HTTPHandler{LockOptions: common.NoLock, Handler: newServer},
	}
}

// IssueTx ...
// TODO: Remove this
func (vm *VM) IssueTx(b []byte, onDecide func(choices.Status)) (ids.ID, error) {
	codec := Codec{}
	tx, err := codec.UnmarshalTx(b)
	if err != nil {
		return ids.ID{}, err
	}
	tx.startVerify(vm.ctx, &vm.factory)
	tx.onDecide = onDecide
	vm.issueTx(tx)
	return tx.id, nil
}

// GetAccount returns the account with the specified address
func (vm *VM) GetAccount(db database.Database, address ids.ShortID) Account {
	if account, err := vm.state.Account(db, address.LongID()); err == nil {
		return account
	}
	builder := Builder{
		NetworkID: 0,
		ChainID:   vm.ctx.ChainID,
	}
	return builder.NewAccount(address, 0, 0)
}

/*
 ******************************************************************************
 ********************************** Timer API *********************************
 ******************************************************************************
 */

// FlushTxs into consensus
func (vm *VM) FlushTxs() {
	vm.timer.Cancel()
	if len(vm.txs) > 0 {
		select {
		case vm.toEngine <- common.PendingTxs:
		default:
			vm.ctx.Log.Warn("Dropping block due to too frequent issuance")
			vm.timer.SetTimeoutIn(batchTimeout)
		}
	}
}

/*
 ******************************************************************************
 ******************************* Implementation *******************************
 ******************************************************************************
 */

// Consensus:

func (vm *VM) initState(genesisBytes []byte) error {
	errs := wrappers.Errs{}

	vdb := versiondb.New(vm.baseDB)

	b := Builder{}
	block, err := b.NewBlock(ids.Empty, nil)
	errs.Add(err)

	c := Codec{}
	accounts, err := c.UnmarshalGenesis(genesisBytes)
	errs.Add(err)
	errs.Add(vm.state.SetBlock(vdb, block.ID(), block))
	errs.Add(vm.state.SetStatus(vdb, block.ID(), choices.Accepted))
	errs.Add(vm.state.SetLastAccepted(vdb, block.ID()))
	for _, account := range accounts {
		errs.Add(vm.state.SetAccount(vdb, account.ID().LongID(), account))
	}
	errs.Add(vm.state.SetDBInitialized(vdb, choices.Processing))

	if errs.Errored() {
		return errs.Err
	}

	return vdb.Commit()
}

func (vm *VM) issueTx(tx *Tx) {
	vm.ctx.Log.Verbo("Issuing tx:\n%s", formatting.DumpBytes{Bytes: tx.Bytes()})

	vm.txs = append(vm.txs, tx)
	switch {
	case len(vm.txs) == maxBatchSize:
		vm.FlushTxs()
	case len(vm.txs) == 1:
		vm.timer.SetTimeoutIn(batchTimeout)
	}
}
