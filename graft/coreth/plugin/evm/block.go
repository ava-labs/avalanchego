// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

var bonusBlocks = ids.Set{}

func init() {
	blockIDStrs := []string{
		"XMoEsew2DhSgQaydcJFJUQAQYP8BTNTYbEJZvtbrV2QsX7iE3",
		"2QiHZwLhQ3xLuyyfcdo5yCUfoSqWDvRZox5ECU19HiswfroCGp",
		"tLLijh7oKfvWT1yk9zRv4FQvuQ5DAiuvb5kHCNN9zh4mqkFMG",
		"2db2wMbVAoCc5EUJrsBYWvNZDekqyY8uNpaaVapdBAQZ5oRaou",
		"2rAsBj3emqQa13CV8r5fTtHogs4sXnjvbbXVzcKPi3WmzhpK9D",
		"amgH2C1s9H3Av7vSW4y7n7TXb9tKyKHENvrDXutgNN6nsejgc",
		"dWBsRYRwFrcyi3DPdLoHsL67QkZ5h86hwtVfP94ZBaY18EkmF",
		"PgaRk1UAoUvRybhnXsrLq5t6imWhEa6ksNjbN6hWgs4qPrSzm",
		"b7XfDDLgwB12DfL7UTWZoxwBpkLPL5mdHtXngD94Y2RoeWXSh",
		"2i2FP6nJyvhX9FR15qN2D9AVoK5XKgBD2i2AQ7FoSpfowxvQDX",
		"2J8z7HNv4nwh82wqRGyEHqQeuw4wJ6mCDCSvUgusBu35asnshK",
		"2cUPPHy1hspr2nAKpQrrAEisLKkaWSS9iF2wjNFyFRs8vnSkKK",
		"2gTygYckZgFZfN5QQWPaPBD3nabqjidV55mwy1x1Nd4JmJAwaM",
		"5MptSdP6dBMPSwk9GJjeVe39deZJTRh9i82cgNibjeDffrrTf",
		"2v3smb35s4GLACsK4Zkd2RcLBLdWA4huqrvq8Y3VP4CVe8kfTM",
		"7KCZKBpxovtX9opb7rMRie9WmW5YbZ8A4HwBBokJ9eSHpZPqx",
		"2oueNTj4dUE2FFtGyPpawnmCCsy6EUQeVHVLZy8NHeQmkAciP4",
		"Nzs93kFTvcXanFUp9Y8VQkKYnzmH8xykxVNFJTkdyAEeuxWbP",
		"2YHZ1KymFjiBhpXzgt6HXJhLSt5SV9UQ4tJuUNjfN1nQQdm5zz",
		"Qv5v5Ru8ArfnWKB1w6s4G5EYPh7TybHJtF6UsVwAkfvZFoqmj",
		"z3BgePPpCXq1mRBRvUi28rYYxnEtJizkUEHnDBrcZeVA7MFVk",
		"Ry2sfjFfGEnJxRkUGFSyZNn7GR3m4aKAf1scDW2uXSNQB568Y",
		"2YgxGHns7Z2hMMHJsPCgVXuJaL7x1b3gnHbmSCfCdyAcYGr6mx",
		"cwJusfmn98TW3DjAbfLRN9utYR24KAQ82qpAXmVSvjHyJZuM2",
		"2JbuExUGKW5mYz5KfXATwq1ibRDimgks9wEdYGNSC6Ttey1R4U",
		"21Jys8UNURmtckKSV89S2hntEWymJszrLQbdLaNcbXcxDAsQSa",
		"MjExz2z1qhwugc1tAyiGxRsCq4GvJwKfyyS29nr4tRVB8ooic",
		"9oZh4qyBCcVwSGyDoUzRAuausvPJN3xH6nopKS6bwYzMfLoQ2",
		"uK5Ff9iBfDtREpVv9NgCQ1STD1nzLJG3yrfibHG4mGvmybw6f",
		"22ck2Z7cC38hmBfX2v3jMWxun8eD8psNaicfYeokS67DxwmPTx",
		"2AfTQ2FXNj9bkSUQnud9pFXULx6EbF7cbbw6i3ayvc2QNhgxfF",
		"pTf7gfk1ksj7bqMrLyMCij8FBKth1uRqQrtfykMFeXhx5xnrL",
		"2AXxT3PSEnaYHNtBTnYrVTf24TtKDWjky9sqoFEhydrGXE9iKH",
		"PJTkRrHvKZ1m4AQdPND1MBpUXpCrGN4DDmXmJQAiUrsxPoLQX",
		"fV8k1U8oQDmfVwK66kAwN73aSsWiWhm8quNpVnKmSznBycV2W",
		"sg6wAwFBsPQiS5Yfyh41cVkCRQbrrXsxXmeNyQ1xkunf2sdyv",
		"soPweZ8DGaoUMjrnzjH3V2bypa7ZvvfqBan4UCsMUxMP759gw",
		"2dNkpQF4mooveyUDfBYQTBfsGDV4wkncQPpEw4kHKfSTSTo5x",
		"63YLdYXfXc5tY3mwWLaDsbXzQHYmwWVxMP7HKbRh4Du3C2iM1",
		"2tCe88ur6MLQcVgwE5XxoaHiTGtSrthwKN3SdbHE4kWiQ7MSTV",
		"2nG4exd9eUoAGzELfksmBR8XDCKhohY1uDKRFzEXJG4M8p3qA7",
		"2F5tSQbdTfhZxvkxZqdFp7KR3FrJPKEsDLQK7KtPhNXj1EZAh4",
		"21o2fVTnzzmtgXqkV1yuQeze7YEQhR5JB31jVVD9oVUnaaV8qm",
		"2pSjfo7rkFCfZ2CqAxqfw8vqM2CU2nVLHrFZe3rwxz43gkVuGo",
		"2QBNMMFJmhVHaGF45GAPszKyj1gK6ToBERRxYvXtM7yfrdUGPK",
		"2ez4CA7w4HHr8SSobHQUAwFgj2giRNjNFUZK9JvrZFa1AuRj6X",
		"2DpCuBaH94zKKFNY2XTs4GeJcwsEv6qT2DHc59S8tdg97GZpcJ",
		"i1HoerJ1axognkUKKL58FvF9aLrbZKtv7TdKLkT5kgzoeU1vB",
		"2SiSziHHqPjb1qkw7CdGYupokiYpd2b7mMqRiyszurctcA5AKr",
		"esx5J962LtYm2aSrskpLai5e4CMMsaS1dsu9iuLGJ3KWgSu2M",
		"2czmtnBS44VCWNRFUM89h4Fe9m3ZeZVYyh7Pe3FhNqjRNgPXhZ",
		"DK9NqAJGry1wAo767uuYc1dYXAjUhzwka6vi8d9tNheqzGUTd",
		"pE93VXY3N5QKfwsEFcM9i59UpPFgeZ8nxpJNaGaDQyDgsscNf",
		"AfWvJH3rB2fdHuPWQp6qYNCFVT29MooQPRigD88rKKwUDEDhq",
		"2KPW9G5tiNF14tZNfG4SqHuQrtUYVZyxuof37aZ7AnTKrQdsHn",
		"BYqLB6xpqy7HsAgP2XNfGE8Ubg1uEzse5mBPTSJH9z5s8pvMa",
		"Njm9TcLUXRojZk8YhEM6ksvfiPdC1TME4zJvGaDXgzMCyB6oB",
	}
	for _, blkIDStr := range blockIDStrs {
		blkID, err := ids.FromString(blkIDStr)
		if err != nil {
			panic(err)
		}
		bonusBlocks.Add(blkID)
	}
}

// Block implements the snowman.Block interface
type Block struct {
	id        ids.ID
	ethBlock  *types.Block
	vm        *VM
	status    choices.Status
	atomicTxs []*Tx
}

// ID implements the snowman.Block interface
func (b *Block) ID() ids.ID { return b.id }

// Accept implements the snowman.Block interface
func (b *Block) Accept() error {
	vm := b.vm

	// Although returning an error from Accept is considered fatal, it is good
	// practice to cleanup the batch we were modifying in the case of an error.
	defer vm.db.Abort()

	b.status = choices.Accepted
	log.Debug(fmt.Sprintf("Accepting block %s (%s) at height %d", b.ID().Hex(), b.ID(), b.Height()))
	if err := vm.chain.Accept(b.ethBlock); err != nil {
		return fmt.Errorf("chain could not accept %s: %w", b.ID(), err)
	}
	if err := vm.acceptedBlockDB.Put(lastAcceptedKey, b.id[:]); err != nil {
		return fmt.Errorf("failed to put %s as the last accepted block: %w", b.ID(), err)
	}

	if len(b.atomicTxs) == 0 {
		return vm.db.Commit()
	}

	batchChainsAndInputs := make(map[ids.ID]*atomic.Requests)

	for _, tx := range b.atomicTxs {
		// Remove the accepted transaction from the mempool
		vm.mempool.RemoveTx(tx.ID())
		// Save the accepted atomic transaction
		if err := vm.writeAtomicTx(b, tx); err != nil {
			return err
		}
		chainID, txRequest, err := tx.UnsignedAtomicTx.Accept()
		if err != nil {
			return err
		}
		// Add/merge in the atomic requests represented by [tx]
		if request, exists := batchChainsAndInputs[chainID]; exists {
			request.PutRequests = append(request.PutRequests, txRequest.PutRequests...)
			request.RemoveRequests = append(request.RemoveRequests, txRequest.RemoveRequests...)
		} else {
			batchChainsAndInputs[chainID] = txRequest
		}
	}

	// If [b] is a bonus block, then we commit the database without applying the requests from
	// the atmoic transactions to shared memory.
	if bonusBlocks.Contains(b.id) {
		log.Info("skipping atomic tx acceptance on bonus block", "block", b.id)
		return vm.db.Commit()
	}

	batch, err := vm.db.CommitBatch()
	if err != nil {
		return fmt.Errorf("failed to create commit batch due to: %w", err)
	}
	return vm.ctx.SharedMemory.Apply(batchChainsAndInputs, batch)
}

// Reject implements the snowman.Block interface
// If [b] contains an atomic transaction, attempt to re-issue it
func (b *Block) Reject() error {
	b.status = choices.Rejected
	log.Debug(fmt.Sprintf("Rejecting block %s (%s) at height %d", b.ID().Hex(), b.ID(), b.Height()))
	for _, tx := range b.atomicTxs {
		b.vm.mempool.RemoveTx(tx.ID())
		if err := b.vm.issueTx(tx, false /* set local to false when re-issuing */); err != nil {
			log.Debug("Failed to re-issue transaction in rejected block", "txID", tx.ID(), "err", err)
		}
	}
	return b.vm.chain.Reject(b.ethBlock)
}

// SetStatus implements the InternalBlock interface allowing ChainState
// to set the status on an existing block
func (b *Block) SetStatus(status choices.Status) { b.status = status }

// Status implements the snowman.Block interface
func (b *Block) Status() choices.Status {
	return b.status
}

// Parent implements the snowman.Block interface
func (b *Block) Parent() ids.ID {
	return ids.ID(b.ethBlock.ParentHash())
}

// Height implements the snowman.Block interface
func (b *Block) Height() uint64 {
	return b.ethBlock.Number().Uint64()
}

// Timestamp implements the snowman.Block interface
func (b *Block) Timestamp() time.Time {
	return time.Unix(int64(b.ethBlock.Time()), 0)
}

// syntacticVerify verifies that a *Block is well-formed.
func (b *Block) syntacticVerify() (params.Rules, error) {
	if b == nil || b.ethBlock == nil {
		return params.Rules{}, errInvalidBlock
	}

	header := b.ethBlock.Header()
	rules := b.vm.chainConfig.AvalancheRules(header.Number, new(big.Int).SetUint64(header.Time))
	return rules, b.vm.getBlockValidator(rules).SyntacticVerify(b)
}

// Verify implements the snowman.Block interface
func (b *Block) Verify() error {
	return b.verify(true)
}

func (b *Block) verify(writes bool) error {
	rules, err := b.syntacticVerify()
	if err != nil {
		return fmt.Errorf("syntactic block verification failed: %w", err)
	}

	if err := b.verifyAtomicTxs(rules); err != nil {
		return err
	}

	return b.vm.chain.BlockChain().InsertBlockManual(b.ethBlock, writes)
}

func (b *Block) verifyAtomicTxs(rules params.Rules) error {
	// Ensure that the parent was verified and inserted correctly.
	ancestorID := b.Parent()
	ancestorHash := common.Hash(ancestorID)
	if !b.vm.chain.BlockChain().HasBlock(ancestorHash, b.Height()-1) {
		return errRejectedParent
	}

	// If the tx is an atomic tx, ensure that it doesn't conflict with any of
	// its processing ancestry.
	inputs := &ids.Set{}
	for _, atomicTx := range b.atomicTxs {
		// If the ancestor is unknown, then the parent failed verification when
		// it was called.
		// If the ancestor is rejected, then this block shouldn't be inserted
		// into the canonical chain because the parent will be missing.
		ancestorInf, err := b.vm.GetBlockInternal(ancestorID)
		if err != nil {
			return errRejectedParent
		}
		if blkStatus := ancestorInf.Status(); blkStatus == choices.Unknown || blkStatus == choices.Rejected {
			return errRejectedParent
		}
		ancestor, ok := ancestorInf.(*Block)
		if !ok {
			return fmt.Errorf("expected %s, parent of %s, to be *Block but is %T", ancestor.ID(), b.ID(), ancestorInf)
		}
		if bonusBlocks.Contains(b.id) {
			log.Info("skipping atomic tx verification on bonus block", "block", b.id)
		} else {
			utx := atomicTx.UnsignedAtomicTx
			if err := utx.SemanticVerify(b.vm, atomicTx, ancestor, b.ethBlock.BaseFee(), rules); err != nil {
				return fmt.Errorf("invalid block due to failed semanatic verify: %w at height %d", err, b.Height())
			}
			txInputs := utx.InputUTXOs()
			if inputs.Overlaps(txInputs) {
				return errConflictingAtomicInputs
			}
			inputs.Union(txInputs)
		}
	}

	return nil
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
