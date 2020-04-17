package wasmvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/snow/choices"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/vms/components/core"
)

// Block is a block of transactions
type Block struct {
	vm          *VM
	*core.Block `serialize:"true"`
	Txs         []tx `serialize:"true"`
}

// Initialize this block
// Should be called when block is parsed from bytes
func (b *Block) Initialize(bytes []byte, vm *VM) {
	b.vm = vm
	b.Block.Initialize(bytes, vm.SnowmanVM)
}

// Accept this block
func (b *Block) Accept() {
	b.Block.Accept()
	for _, tx := range b.Txs {
		tx.Accept()
		b.vm.putTxStatus(b.vm.DB, tx.ID(), choices.Accepted)
	}
	b.vm.DB.Commit()
}

// Verify returns nil iff this block is valid
func (b *Block) Verify() error {
	switch {
	case b.ID().Equals(ids.Empty):
		return errors.New("block ID is empty")
	case len(b.Txs) == 0:
		return errors.New("no txs in block")
	}

	// TODO: If there's an error, return other txs to mempool
	for _, tx := range b.Txs {
		if err := tx.SyntacticVerify(); err != nil {
			return err
		}
	}

	// TODO: If there's an error, return other txs to mempool
	for _, tx := range b.Txs {
		if err := tx.SemanticVerify(nil); err != nil { // TODO pass DB here
			return err
		}
	}

	return nil
}

// return a new, initialized block
func (vm *VM) newBlock(parentID ids.ID, txs []tx) (*Block, error) {
	block := &Block{
		Block: core.NewBlock(parentID),
		Txs:   txs,
	}

	bytes, err := codec.Marshal(block)
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal block: %s", err)
	}
	block.Initialize(bytes, vm)

	if err := vm.SaveBlock(vm.DB, block); err != nil {
		return nil, fmt.Errorf("couldn't save block %s: %s", block.ID(), err)
	}
	if err := vm.DB.Commit(); err != nil {
		return nil, fmt.Errorf("couldn't commit DB: %s", err)
	}
	return block, nil
}
