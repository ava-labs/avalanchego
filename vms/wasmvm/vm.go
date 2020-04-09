package wasmvm

import (
	"errors"
	"fmt"
	"io/ioutil"

	"github.com/ava-labs/gecko/ids"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/consensus/snowman"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/vms/components/core"
)

// VM defines the Salesforce Chain
type VM struct {
	*core.SnowmanVM

	// txs not yet proposed
	mempool []tx

	// Key: Contract ID
	// Value: Byte repr. of the Wasm
	contracts map[[32]byte][]byte
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

	// Inititalize data structures
	vm.contracts = make(map[[32]byte][]byte, 0)

	wasmBytes, err := ioutil.ReadFile("/home/danlaine/go/src/github.com/ava-labs/gecko/vms/wasmvm/contracts/counter/counter.wasm")
	if err != nil {
		return fmt.Errorf("couldn't find contract")
	}

	if !vm.DBInitialized() {
		ctx.Log.Debug("initializing state from genesis bytes") // TODO delete
		genesisTx, err := vm.newCreateContractTx(
			wasmBytes,
		)
		if err != nil {
			return fmt.Errorf("couldn't make genesis tx: %v", err)
		}
		genesisTx.ID = ids.Empty
		genesisBlock, err := vm.newBlock(ids.Empty, []tx{genesisTx})
		if err != nil {
			return fmt.Errorf("couldn't make genesis block: %v", err)
		}
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
		tx.initialize(vm)
	}
	return &block, nil
}
