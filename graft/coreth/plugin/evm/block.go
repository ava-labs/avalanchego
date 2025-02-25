// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/plugin/evm/header"
	"github.com/ava-labs/coreth/precompile/precompileconfig"
	"github.com/ava-labs/coreth/predicate"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var (
	_ snowman.Block           = (*Block)(nil)
	_ block.WithVerifyContext = (*Block)(nil)
)

var errMissingUTXOs = errors.New("missing UTXOs")

// readMainnetBonusBlocks returns maps of bonus block numbers to block IDs.
// Note bonus blocks are indexed in the atomic trie.
func readMainnetBonusBlocks() (map[uint64]ids.ID, error) {
	mainnetBonusBlocks := map[uint64]string{
		102972: "Njm9TcLUXRojZk8YhEM6ksvfiPdC1TME4zJvGaDXgzMCyB6oB",
		103105: "BYqLB6xpqy7HsAgP2XNfGE8Ubg1uEzse5mBPTSJH9z5s8pvMa",
		103143: "AfWvJH3rB2fdHuPWQp6qYNCFVT29MooQPRigD88rKKwUDEDhq",
		103183: "2KPW9G5tiNF14tZNfG4SqHuQrtUYVZyxuof37aZ7AnTKrQdsHn",
		103197: "pE93VXY3N5QKfwsEFcM9i59UpPFgeZ8nxpJNaGaDQyDgsscNf",
		103203: "2czmtnBS44VCWNRFUM89h4Fe9m3ZeZVYyh7Pe3FhNqjRNgPXhZ",
		103208: "esx5J962LtYm2aSrskpLai5e4CMMsaS1dsu9iuLGJ3KWgSu2M",
		103209: "DK9NqAJGry1wAo767uuYc1dYXAjUhzwka6vi8d9tNheqzGUTd",
		103259: "i1HoerJ1axognkUKKL58FvF9aLrbZKtv7TdKLkT5kgzoeU1vB",
		103261: "2DpCuBaH94zKKFNY2XTs4GeJcwsEv6qT2DHc59S8tdg97GZpcJ",
		103266: "2ez4CA7w4HHr8SSobHQUAwFgj2giRNjNFUZK9JvrZFa1AuRj6X",
		103287: "2QBNMMFJmhVHaGF45GAPszKyj1gK6ToBERRxYvXtM7yfrdUGPK",
		103339: "2pSjfo7rkFCfZ2CqAxqfw8vqM2CU2nVLHrFZe3rwxz43gkVuGo",
		103346: "2SiSziHHqPjb1qkw7CdGYupokiYpd2b7mMqRiyszurctcA5AKr",
		103350: "2F5tSQbdTfhZxvkxZqdFp7KR3FrJPKEsDLQK7KtPhNXj1EZAh4",
		103358: "2tCe88ur6MLQcVgwE5XxoaHiTGtSrthwKN3SdbHE4kWiQ7MSTV",
		103437: "21o2fVTnzzmtgXqkV1yuQeze7YEQhR5JB31jVVD9oVUnaaV8qm",
		103472: "2nG4exd9eUoAGzELfksmBR8XDCKhohY1uDKRFzEXJG4M8p3qA7",
		103478: "63YLdYXfXc5tY3mwWLaDsbXzQHYmwWVxMP7HKbRh4Du3C2iM1",
		103493: "soPweZ8DGaoUMjrnzjH3V2bypa7ZvvfqBan4UCsMUxMP759gw",
		103514: "2dNkpQF4mooveyUDfBYQTBfsGDV4wkncQPpEw4kHKfSTSTo5x",
		103536: "PJTkRrHvKZ1m4AQdPND1MBpUXpCrGN4DDmXmJQAiUrsxPoLQX",
		103545: "22ck2Z7cC38hmBfX2v3jMWxun8eD8psNaicfYeokS67DxwmPTx",
		103547: "pTf7gfk1ksj7bqMrLyMCij8FBKth1uRqQrtfykMFeXhx5xnrL",
		103554: "9oZh4qyBCcVwSGyDoUzRAuausvPJN3xH6nopKS6bwYzMfLoQ2",
		103555: "MjExz2z1qhwugc1tAyiGxRsCq4GvJwKfyyS29nr4tRVB8ooic",
		103559: "cwJusfmn98TW3DjAbfLRN9utYR24KAQ82qpAXmVSvjHyJZuM2",
		103561: "2YgxGHns7Z2hMMHJsPCgVXuJaL7x1b3gnHbmSCfCdyAcYGr6mx",
		103563: "2AXxT3PSEnaYHNtBTnYrVTf24TtKDWjky9sqoFEhydrGXE9iKH",
		103564: "Ry2sfjFfGEnJxRkUGFSyZNn7GR3m4aKAf1scDW2uXSNQB568Y",
		103569: "21Jys8UNURmtckKSV89S2hntEWymJszrLQbdLaNcbXcxDAsQSa",
		103570: "sg6wAwFBsPQiS5Yfyh41cVkCRQbrrXsxXmeNyQ1xkunf2sdyv",
		103575: "z3BgePPpCXq1mRBRvUi28rYYxnEtJizkUEHnDBrcZeVA7MFVk",
		103577: "uK5Ff9iBfDtREpVv9NgCQ1STD1nzLJG3yrfibHG4mGvmybw6f",
		103578: "Qv5v5Ru8ArfnWKB1w6s4G5EYPh7TybHJtF6UsVwAkfvZFoqmj",
		103582: "7KCZKBpxovtX9opb7rMRie9WmW5YbZ8A4HwBBokJ9eSHpZPqx",
		103587: "2AfTQ2FXNj9bkSUQnud9pFXULx6EbF7cbbw6i3ayvc2QNhgxfF",
		103590: "2gTygYckZgFZfN5QQWPaPBD3nabqjidV55mwy1x1Nd4JmJAwaM",
		103591: "2cUPPHy1hspr2nAKpQrrAEisLKkaWSS9iF2wjNFyFRs8vnSkKK",
		103594: "5MptSdP6dBMPSwk9GJjeVe39deZJTRh9i82cgNibjeDffrrTf",
		103597: "2J8z7HNv4nwh82wqRGyEHqQeuw4wJ6mCDCSvUgusBu35asnshK",
		103598: "2i2FP6nJyvhX9FR15qN2D9AVoK5XKgBD2i2AQ7FoSpfowxvQDX",
		103603: "2v3smb35s4GLACsK4Zkd2RcLBLdWA4huqrvq8Y3VP4CVe8kfTM",
		103604: "b7XfDDLgwB12DfL7UTWZoxwBpkLPL5mdHtXngD94Y2RoeWXSh",
		103607: "PgaRk1UAoUvRybhnXsrLq5t6imWhEa6ksNjbN6hWgs4qPrSzm",
		103612: "2oueNTj4dUE2FFtGyPpawnmCCsy6EUQeVHVLZy8NHeQmkAciP4",
		103614: "2YHZ1KymFjiBhpXzgt6HXJhLSt5SV9UQ4tJuUNjfN1nQQdm5zz",
		103617: "amgH2C1s9H3Av7vSW4y7n7TXb9tKyKHENvrDXutgNN6nsejgc",
		103618: "fV8k1U8oQDmfVwK66kAwN73aSsWiWhm8quNpVnKmSznBycV2W",
		103621: "Nzs93kFTvcXanFUp9Y8VQkKYnzmH8xykxVNFJTkdyAEeuxWbP",
		103623: "2rAsBj3emqQa13CV8r5fTtHogs4sXnjvbbXVzcKPi3WmzhpK9D",
		103624: "2JbuExUGKW5mYz5KfXATwq1ibRDimgks9wEdYGNSC6Ttey1R4U",
		103627: "tLLijh7oKfvWT1yk9zRv4FQvuQ5DAiuvb5kHCNN9zh4mqkFMG",
		103628: "dWBsRYRwFrcyi3DPdLoHsL67QkZ5h86hwtVfP94ZBaY18EkmF",
		103629: "XMoEsew2DhSgQaydcJFJUQAQYP8BTNTYbEJZvtbrV2QsX7iE3",
		103630: "2db2wMbVAoCc5EUJrsBYWvNZDekqyY8uNpaaVapdBAQZ5oRaou",
		103633: "2QiHZwLhQ3xLuyyfcdo5yCUfoSqWDvRZox5ECU19HiswfroCGp",
	}

	bonusBlockMainnetHeights := make(map[uint64]ids.ID)
	for height, blkIDStr := range mainnetBonusBlocks {
		blkID, err := ids.FromString(blkIDStr)
		if err != nil {
			return nil, err
		}
		bonusBlockMainnetHeights[height] = blkID
	}
	return bonusBlockMainnetHeights, nil
}

// Block implements the snowman.Block interface
type Block struct {
	id        ids.ID
	ethBlock  *types.Block
	vm        *VM
	atomicTxs []*atomic.Tx
}

// newBlock returns a new Block wrapping the ethBlock type and implementing the snowman.Block interface
func (vm *VM) newBlock(ethBlock *types.Block) (*Block, error) {
	isApricotPhase5 := vm.chainConfig.IsApricotPhase5(ethBlock.Time())
	atomicTxs, err := atomic.ExtractAtomicTxs(ethBlock.ExtData(), isApricotPhase5, atomic.Codec)
	if err != nil {
		return nil, err
	}

	return &Block{
		id:        ids.ID(ethBlock.Hash()),
		ethBlock:  ethBlock,
		vm:        vm,
		atomicTxs: atomicTxs,
	}, nil
}

// ID implements the snowman.Block interface
func (b *Block) ID() ids.ID { return b.id }

func (b *Block) AtomicTxs() []*atomic.Tx { return b.atomicTxs }

// Accept implements the snowman.Block interface
func (b *Block) Accept(context.Context) error {
	vm := b.vm

	// Although returning an error from Accept is considered fatal, it is good
	// practice to cleanup the batch we were modifying in the case of an error.
	defer vm.versiondb.Abort()

	log.Debug(fmt.Sprintf("Accepting block %s (%s) at height %d", b.ID().Hex(), b.ID(), b.Height()))

	// Call Accept for relevant precompile logs. Note we do this prior to
	// calling Accept on the blockChain so any side effects (eg warp signatures)
	// take place before the accepted log is emitted to subscribers.
	rules := b.vm.chainConfig.Rules(b.ethBlock.Number(), b.ethBlock.Timestamp())
	if err := b.handlePrecompileAccept(rules); err != nil {
		return err
	}
	if err := vm.blockChain.Accept(b.ethBlock); err != nil {
		return fmt.Errorf("chain could not accept %s: %w", b.ID(), err)
	}

	if err := vm.acceptedBlockDB.Put(lastAcceptedKey, b.id[:]); err != nil {
		return fmt.Errorf("failed to put %s as the last accepted block: %w", b.ID(), err)
	}

	for _, tx := range b.atomicTxs {
		// Remove the accepted transaction from the mempool
		vm.mempool.RemoveTx(tx)
	}

	// Update VM state for atomic txs in this block. This includes updating the
	// atomic tx repo, atomic trie, and shared memory.
	atomicState, err := b.vm.atomicBackend.GetVerifiedAtomicState(common.Hash(b.ID()))
	if err != nil {
		// should never occur since [b] must be verified before calling Accept
		return err
	}
	// Get pending operations on the vm's versionDB so we can apply them atomically
	// with the shared memory changes.
	vdbBatch, err := b.vm.versiondb.CommitBatch()
	if err != nil {
		return fmt.Errorf("could not create commit batch processing block[%s]: %w", b.ID(), err)
	}

	// Apply any shared memory changes atomically with other pending changes to
	// the vm's versionDB.
	return atomicState.Accept(vdbBatch, nil)
}

// handlePrecompileAccept calls Accept on any logs generated with an active precompile address that implements
// contract.Accepter
func (b *Block) handlePrecompileAccept(rules params.Rules) error {
	// Short circuit early if there are no precompile accepters to execute
	if len(rules.AccepterPrecompiles) == 0 {
		return nil
	}

	// Read receipts from disk
	receipts := rawdb.ReadReceipts(b.vm.chaindb, b.ethBlock.Hash(), b.ethBlock.NumberU64(), b.ethBlock.Time(), b.vm.chainConfig)
	// If there are no receipts, ReadReceipts may be nil, so we check the length and confirm the ReceiptHash
	// is empty to ensure that missing receipts results in an error on accept.
	if len(receipts) == 0 && b.ethBlock.ReceiptHash() != types.EmptyRootHash {
		return fmt.Errorf("failed to fetch receipts for accepted block with non-empty root hash (%s) (Block: %s, Height: %d)", b.ethBlock.ReceiptHash(), b.ethBlock.Hash(), b.ethBlock.NumberU64())
	}
	acceptCtx := &precompileconfig.AcceptContext{
		SnowCtx: b.vm.ctx,
		Warp:    b.vm.warpBackend,
	}
	for _, receipt := range receipts {
		for logIdx, log := range receipt.Logs {
			accepter, ok := rules.AccepterPrecompiles[log.Address]
			if !ok {
				continue
			}
			if err := accepter.Accept(acceptCtx, log.BlockHash, log.BlockNumber, log.TxHash, logIdx, log.Topics, log.Data); err != nil {
				return err
			}
		}
	}

	return nil
}

// Reject implements the snowman.Block interface
// If [b] contains an atomic transaction, attempt to re-issue it
func (b *Block) Reject(context.Context) error {
	log.Debug(fmt.Sprintf("Rejecting block %s (%s) at height %d", b.ID().Hex(), b.ID(), b.Height()))
	for _, tx := range b.atomicTxs {
		// Re-issue the transaction in the mempool, continue even if it fails
		b.vm.mempool.RemoveTx(tx)
		if err := b.vm.mempool.AddRemoteTx(tx); err != nil {
			log.Debug("Failed to re-issue transaction in rejected block", "txID", tx.ID(), "err", err)
		}
	}
	atomicState, err := b.vm.atomicBackend.GetVerifiedAtomicState(common.Hash(b.ID()))
	if err != nil {
		// should never occur since [b] must be verified before calling Reject
		return err
	}
	if err := atomicState.Reject(); err != nil {
		return err
	}
	return b.vm.blockChain.Reject(b.ethBlock)
}

// Parent implements the snowman.Block interface
func (b *Block) Parent() ids.ID {
	return ids.ID(b.ethBlock.ParentHash())
}

// Height implements the snowman.Block interface
func (b *Block) Height() uint64 {
	return b.ethBlock.NumberU64()
}

// Timestamp implements the snowman.Block interface
func (b *Block) Timestamp() time.Time {
	return time.Unix(int64(b.ethBlock.Time()), 0)
}

// syntacticVerify verifies that a *Block is well-formed.
func (b *Block) syntacticVerify() error {
	if b == nil || b.ethBlock == nil {
		return errInvalidBlock
	}

	header := b.ethBlock.Header()
	rules := b.vm.chainConfig.Rules(header.Number, header.Time)
	return b.vm.syntacticBlockValidator.SyntacticVerify(b, rules)
}

// Verify implements the snowman.Block interface
func (b *Block) Verify(context.Context) error {
	return b.verify(&precompileconfig.PredicateContext{
		SnowCtx:            b.vm.ctx,
		ProposerVMBlockCtx: nil,
	}, true)
}

// ShouldVerifyWithContext implements the block.WithVerifyContext interface
func (b *Block) ShouldVerifyWithContext(context.Context) (bool, error) {
	predicates := b.vm.chainConfig.Rules(b.ethBlock.Number(), b.ethBlock.Timestamp()).Predicaters
	// Short circuit early if there are no predicates to verify
	if len(predicates) == 0 {
		return false, nil
	}

	// Check if any of the transactions in the block specify a precompile that enforces a predicate, which requires
	// the ProposerVMBlockCtx.
	for _, tx := range b.ethBlock.Transactions() {
		for _, accessTuple := range tx.AccessList() {
			if _, ok := predicates[accessTuple.Address]; ok {
				log.Debug("Block verification requires proposerVM context", "block", b.ID(), "height", b.Height())
				return true, nil
			}
		}
	}

	log.Debug("Block verification does not require proposerVM context", "block", b.ID(), "height", b.Height())
	return false, nil
}

// VerifyWithContext implements the block.WithVerifyContext interface
func (b *Block) VerifyWithContext(ctx context.Context, proposerVMBlockCtx *block.Context) error {
	return b.verify(&precompileconfig.PredicateContext{
		SnowCtx:            b.vm.ctx,
		ProposerVMBlockCtx: proposerVMBlockCtx,
	}, true)
}

// Verify the block is valid.
// Enforces that the predicates are valid within [predicateContext].
// Writes the block details to disk and the state to the trie manager iff writes=true.
func (b *Block) verify(predicateContext *precompileconfig.PredicateContext, writes bool) error {
	if predicateContext.ProposerVMBlockCtx != nil {
		log.Debug("Verifying block with context", "block", b.ID(), "height", b.Height())
	} else {
		log.Debug("Verifying block without context", "block", b.ID(), "height", b.Height())
	}
	if err := b.syntacticVerify(); err != nil {
		return fmt.Errorf("syntactic block verification failed: %w", err)
	}

	// verify UTXOs named in import txs are present in shared memory.
	if err := b.verifyUTXOsPresent(); err != nil {
		return err
	}

	// Only enforce predicates if the chain has already bootstrapped.
	// If the chain is still bootstrapping, we can assume that all blocks we are verifying have
	// been accepted by the network (so the predicate was validated by the network when the
	// block was originally verified).
	if b.vm.bootstrapped.Get() {
		if err := b.verifyPredicates(predicateContext); err != nil {
			return fmt.Errorf("failed to verify predicates: %w", err)
		}
	}

	// The engine may call VerifyWithContext multiple times on the same block with different contexts.
	// Since the engine will only call Accept/Reject once, we should only call InsertBlockManual once.
	// Additionally, if a block is already in processing, then it has already passed verification and
	// at this point we have checked the predicates are still valid in the different context so we
	// can return nil.
	if b.vm.State.IsProcessing(b.id) {
		return nil
	}

	err := b.vm.blockChain.InsertBlockManual(b.ethBlock, writes)
	if err != nil || !writes {
		// if an error occurred inserting the block into the chain
		// or if we are not pinning to memory, unpin the atomic trie
		// changes from memory (if they were pinned).
		if atomicState, err := b.vm.atomicBackend.GetVerifiedAtomicState(b.ethBlock.Hash()); err == nil {
			_ = atomicState.Reject() // ignore this error so we can return the original error instead.
		}
	}
	return err
}

// verifyPredicates verifies the predicates in the block are valid according to predicateContext.
func (b *Block) verifyPredicates(predicateContext *precompileconfig.PredicateContext) error {
	rules := b.vm.chainConfig.Rules(b.ethBlock.Number(), b.ethBlock.Timestamp())

	switch {
	case !rules.IsDurango && rules.PredicatersExist():
		return errors.New("cannot enable predicates before Durango activation")
	case !rules.IsDurango:
		return nil
	}

	predicateResults := predicate.NewResults()
	for _, tx := range b.ethBlock.Transactions() {
		results, err := core.CheckPredicates(rules, predicateContext, tx)
		if err != nil {
			return err
		}
		predicateResults.SetTxResults(tx.Hash(), results)
	}
	// TODO: document required gas constraints to ensure marshalling predicate results does not error
	predicateResultsBytes, err := predicateResults.Bytes()
	if err != nil {
		return fmt.Errorf("failed to marshal predicate results: %w", err)
	}
	extraData := b.ethBlock.Extra()
	headerPredicateResultsBytes := header.PredicateBytesFromExtra(extraData)
	if !bytes.Equal(headerPredicateResultsBytes, predicateResultsBytes) {
		return fmt.Errorf("%w (remote: %x local: %x)", errInvalidHeaderPredicateResults, headerPredicateResultsBytes, predicateResultsBytes)
	}
	return nil
}

// verifyUTXOsPresent returns an error if any of the atomic transactions name UTXOs that
// are not present in shared memory.
func (b *Block) verifyUTXOsPresent() error {
	blockHash := common.Hash(b.ID())
	if b.vm.atomicBackend.IsBonus(b.Height(), blockHash) {
		log.Info("skipping atomic tx verification on bonus block", "block", blockHash)
		return nil
	}

	if !b.vm.bootstrapped.Get() {
		return nil
	}

	// verify UTXOs named in import txs are present in shared memory.
	for _, atomicTx := range b.atomicTxs {
		utx := atomicTx.UnsignedAtomicTx
		chainID, requests, err := utx.AtomicOps()
		if err != nil {
			return err
		}
		if _, err := b.vm.ctx.SharedMemory.Get(chainID, requests.RemoveRequests); err != nil {
			return fmt.Errorf("%w: %s", errMissingUTXOs, err)
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
