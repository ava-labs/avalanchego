// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rlp"

	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/params/extras"
	"github.com/ava-labs/subnet-evm/plugin/evm/header"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/subnet-evm/predicate"
)

var (
	_ snowman.Block           = (*Block)(nil)
	_ block.WithVerifyContext = (*Block)(nil)
)

// Block implements the snowman.Block interface
type Block struct {
	id       ids.ID
	ethBlock *types.Block
	vm       *VM
}

// newBlock returns a new Block wrapping the ethBlock type and implementing the snowman.Block interface
func (vm *VM) newBlock(ethBlock *types.Block) *Block {
	return &Block{
		id:       ids.ID(ethBlock.Hash()),
		ethBlock: ethBlock,
		vm:       vm,
	}
}

// ID implements the snowman.Block interface
func (b *Block) ID() ids.ID { return b.id }

// Accept implements the snowman.Block interface
func (b *Block) Accept(context.Context) error {
	vm := b.vm

	// Although returning an error from Accept is considered fatal, it is good
	// practice to cleanup the batch we were modifying in the case of an error.
	defer vm.versiondb.Abort()

	blkID := b.ID()
	log.Debug("accepting block",
		"hash", blkID.Hex(),
		"id", blkID,
		"height", b.Height(),
	)

	// Call Accept for relevant precompile logs. Note we do this prior to
	// calling Accept on the blockChain so any side effects (eg warp signatures)
	// take place before the accepted log is emitted to subscribers.
	rules := b.vm.rules(b.ethBlock.Number(), b.ethBlock.Time())
	if err := b.handlePrecompileAccept(rules); err != nil {
		return err
	}
	if err := vm.blockChain.Accept(b.ethBlock); err != nil {
		return fmt.Errorf("chain could not accept %s: %w", blkID, err)
	}

	if err := vm.acceptedBlockDB.Put(lastAcceptedKey, blkID[:]); err != nil {
		return fmt.Errorf("failed to put %s as the last accepted block: %w", blkID, err)
	}

	return b.vm.versiondb.Commit()
}

// handlePrecompileAccept calls Accept on any logs generated with an active precompile address that implements
// contract.Accepter
func (b *Block) handlePrecompileAccept(rules extras.Rules) error {
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
func (b *Block) Reject(context.Context) error {
	blkID := b.ID()
	log.Debug("rejecting block",
		"hash", blkID.Hex(),
		"id", blkID,
		"height", b.Height(),
	)
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
	rules := b.vm.chainConfig.Rules(header.Number, params.IsMergeTODO, header.Time)
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
	rules := b.vm.rules(b.ethBlock.Number(), b.ethBlock.Time())
	predicates := rules.Predicaters
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

	return b.vm.blockChain.InsertBlockManual(b.ethBlock, writes)
}

// verifyPredicates verifies the predicates in the block are valid according to predicateContext.
func (b *Block) verifyPredicates(predicateContext *precompileconfig.PredicateContext) error {
	rules := b.vm.chainConfig.Rules(b.ethBlock.Number(), params.IsMergeTODO, b.ethBlock.Time())
	rulesExtra := params.GetRulesExtra(rules)

	switch {
	case !rulesExtra.IsDurango && rulesExtra.PredicatersExist():
		return errors.New("cannot enable predicates before Durango activation")
	case !rulesExtra.IsDurango:
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

// Bytes implements the snowman.Block interface
func (b *Block) Bytes() []byte {
	res, err := rlp.EncodeToBytes(b.ethBlock)
	if err != nil {
		panic(err)
	}
	return res
}

func (b *Block) String() string { return fmt.Sprintf("EVM block, ID = %s", b.ID()) }
