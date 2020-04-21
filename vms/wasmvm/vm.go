package wasmvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/database/versiondb"

	"github.com/ava-labs/gecko/cache"
	"github.com/ava-labs/gecko/ids"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/consensus/snowman"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/vms/components/core"
)

const cacheSize = 128

// VM defines the Salesforce Chain
type VM struct {
	*core.SnowmanVM

	// txs not yet proposed
	mempool []tx

	// Key: Contract ID
	// Value: Smart contract (*wasm.Instance)
	contracts cache.LRUCloser

	// For contracts to read/write from
	// TODO: Give each contract its own db
	contractDB database.Database
}

// Initialize this chain
func (vm *VM) Initialize(
	ctx *snow.Context,
	db database.Database,
	_ []byte, // no genesis data
	msgs chan<- common.Message,
	_ []*common.Fx,
) error {
	ctx.Log.Debug("initiailizing wasm chain")

	if err := vm.SnowmanVM.Initialize(ctx, db, vm.ParseBlock, msgs); err != nil {
		return fmt.Errorf("could initialize snowmanVM: %s", err)
	}
	if err := vm.registerDBTypes(); err != nil {
		return fmt.Errorf("error initializing state: %v", err)
	}
	vm.contracts = cache.LRUCloser{Size: cacheSize}

	if !vm.DBInitialized() {
		ctx.Log.Debug("initializing state from genesis bytes")
		// manually create genesis block
		genesisBlock := &Block{
			vm:         vm,
			Block:      core.NewBlock(ids.Empty),
			Txs:        []tx{},
			onAcceptDb: versiondb.New(vm.DB),
		}
		genesisBlockBytes := genesisBlock.Bytes()
		genesisBlock.Block.Initialize(genesisBlockBytes, vm.SnowmanVM)
		genesisBlock.Accept()
		vm.SetDBInitialized()
		vm.DB.Commit()
	}

	return nil
}

// Shutdown this chain
func (vm *VM) Shutdown() {
	vm.DB.Commit()
	vm.DB.Close()
}

// BuildBlock returns a block to propose
// Right now blocks have only 1 tx in them
func (vm *VM) BuildBlock() (snowman.Block, error) {
	if len(vm.mempool) < 1 {
		return nil, errors.New("no transactions to propose")
	}

	// TODO: Have blocks contain >1 tx
	var proposedTx tx
	proposedTx, vm.mempool = vm.mempool[0], vm.mempool[1:]
	if len(vm.mempool) != 0 {
		defer vm.NotifyBlockReady()
	}

	block, err := vm.newBlock(vm.Preferred(), []tx{proposedTx})
	if err != nil {
		return nil, fmt.Errorf("couldn't create new block: %s", err)
	}

	return block, nil
}

// ParseBlock from bytes
func (vm *VM) ParseBlock(bytes []byte) (snowman.Block, error) {
	var block Block
	if err := codec.Unmarshal(bytes, &block); err != nil {
		return nil, fmt.Errorf("couldn't parse block: %s", err)
	}
	block.Initialize(bytes, vm)
	for _, tx := range block.Txs {
		if err := tx.initialize(vm); err != nil {
			return nil, fmt.Errorf("error initializing tx: %s", err)
		}
	}
	return &block, nil
}
