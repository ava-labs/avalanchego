// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/components/missing"
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
	vm := b.vm

	log.Trace(fmt.Sprintf("Block %s is accepted", b.ID()))
	vm.updateStatus(b.id, choices.Accepted)
	if err := vm.acceptedDB.Put(b.ethBlock.Number().Bytes(), b.id[:]); err != nil {
		return err
	}

	tx := vm.getAtomicTx(b.ethBlock)
	if tx == nil {
		return nil
	}
	utx, ok := tx.UnsignedTx.(UnsignedAtomicTx)
	if !ok {
		return errUnknownAtomicTx
	}

	atxErr := utx.Accept(vm.ctx, nil)
	if atxErr == nil {
		return nil
	}

	switch utx.(type) {
	case *UnsignedImportTx:
		// TODO: If error is removed and bonus block, return nil
		return nil
	case *UnsignedExportTx:
		return atxErr
	default:
		return errUnknownAtomicTx
	}
}

// Reject implements the snowman.Block interface
func (b *Block) Reject() error {
	log.Trace(fmt.Sprintf("Block %s is rejected", b.ID()))
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
	parentID := ids.ID(b.ethBlock.ParentHash())
	if block := b.vm.getBlock(parentID); block != nil {
		return block
	}
	return &missing.Block{BlkID: parentID}
}

// Height implements the snowman.Block interface
func (b *Block) Height() uint64 {
	return b.ethBlock.Number().Uint64()
}

// Verify implements the snowman.Block interface
func (b *Block) Verify() error {
	vm := b.vm

	// Only enforce a minimum fee when bootstrapping has finished
	if vm.ctx.IsBootstrapped() {
		// Ensure the minimum gas price is paid for every transaction
		for _, tx := range b.ethBlock.Transactions() {
			if tx.GasPrice().Cmp(params.MinGasPrice) < 0 {
				return errInvalidGas
			}
		}
	}

	// If the tx is an atomic tx, ensure that it doesn't conflict with any of
	// its processing ancestry.
	tx := vm.getAtomicTx(b.ethBlock)
	if tx != nil {
		ancestor := b.Parent().(*Block)
		parentState, err := vm.chain.BlockState(ancestor.ethBlock)
		if err != nil {
			return err
		}
		switch atx := tx.UnsignedTx.(type) {
		case *UnsignedImportTx:
			// If an import tx is seen, we must ensure that none of the
			// processing ancestors consume the same UTXO.
			inputs := atx.InputUTXOs()
			for ancestor.Status() != choices.Accepted {
				atx := vm.getAtomicTx(ancestor.ethBlock)
				// If the ancestor isn't an atomic block, it can't conflict with
				// the import tx.
				if atx != nil {
					ancestorInputs := atx.UnsignedTx.(UnsignedAtomicTx).InputUTXOs()
					if inputs.Overlaps(ancestorInputs) {
						return errConflictingAtomicInputs
					}
				}

				// Move up the chain.
				ancestor = ancestor.Parent().(*Block)
			}
		case *UnsignedExportTx:
			// Export txs are validated by the processor's nonce management.
		default:
			return errUnknownAtomicTx
		}

		// We have verified that none of the processing ancestors conflict with
		// the atomic transaction, so now we must ensure that the transaction is
		// valid and doesn't have any accepted conflicts.

		utx := tx.UnsignedTx.(UnsignedAtomicTx)
		if err := utx.SemanticVerify(vm, tx); err != nil {
			return fmt.Errorf("invalid block due to failed semanatic verify: %w at height %d", err, b.Height())
		}

		// TODO: Because InsertChain calls Process, can't this invocation be removed?
		bc := vm.chain.BlockChain()
		_, _, _, err = bc.Processor().Process(b.ethBlock, parentState, *bc.GetVMConfig())
		if err != nil {
			return fmt.Errorf("invalid block due to failed processing: %w", err)
		}
	}
	_, err := vm.chain.InsertChain([]*types.Block{b.ethBlock})
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
