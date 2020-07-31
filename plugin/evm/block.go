// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/go-ethereum/rlp"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowman"
)

// Block implements the snowman.Block interface
type Block struct {
	id       ids.ID
	ethBlock *types.Block
	vm       *VM
}

// ID implements the snowman.Block interface
func (b *Block) ID() ids.ID { return b.id }

// Accept implements the snowman.Block interface
func (b *Block) Accept() error {
	b.vm.ctx.Log.Verbo("Block %s is accepted", b.ID())
	b.vm.updateStatus(b.ID(), choices.Accepted)
	return nil
}

// Reject implements the snowman.Block interface
func (b *Block) Reject() error {
	b.vm.ctx.Log.Verbo("Block %s is rejected", b.ID())
	b.vm.updateStatus(b.ID(), choices.Rejected)
	return nil
}

// Status implements the snowman.Block interface
func (b *Block) Status() choices.Status {
	status := b.vm.getCachedStatus(b.ID())
	if status == choices.Unknown && b.ethBlock != nil {
		return choices.Processing
	}
	return status
}

// Parent implements the snowman.Block interface
func (b *Block) Parent() snowman.Block {
	parentID := ids.NewID(b.ethBlock.ParentHash())
	block := &Block{
		id:       parentID,
		ethBlock: b.vm.getCachedBlock(parentID),
		vm:       b.vm,
	}
	b.vm.ctx.Log.Verbo("Parent(%s) has status: %s", block.ID(), block.Status())
	return block
}

// Verify implements the snowman.Block interface
func (b *Block) Verify() error {
	// Ensure the minimum gas price is paid for every transaction
	for _, tx := range b.ethBlock.Transactions() {
		if tx.GasPrice().Cmp(minGasPrice) < 0 {
			return errInvalidBlock
		}
	}

	// Attempt to add the block to the chain and consider the block valid iff it's
	// accepted
	_, err := b.vm.chain.InsertChain([]*types.Block{b.ethBlock})
	return err
}

// Bytes implements the snowman.Block interface
func (b *Block) Bytes() []byte {
	res, err := rlp.EncodeToBytes(b.ethBlock)
	if err != nil {
		panic(err)
	}
	return res
}

func (b *Block) String() string { return fmt.Sprintf("EVM block, ID = %s", b.ID()) }
