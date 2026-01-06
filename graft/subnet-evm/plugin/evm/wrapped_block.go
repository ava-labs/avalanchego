// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customheader"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/extension"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/math"
)

var (
	_ snowman.Block           = (*wrappedBlock)(nil)
	_ block.WithVerifyContext = (*wrappedBlock)(nil)
	_ extension.ExtendedBlock = (*wrappedBlock)(nil)

	errMissingParentBlock                  = errors.New("missing parent block")
	errInvalidGasUsedRelativeToCapacity    = errors.New("invalid gas used relative to capacity")
	errTotalIntrinsicGasCostExceedsClaimed = errors.New("total intrinsic gas cost is greater than claimed gas used")
)

// Sentinel errors for header validation in this file
var (
	errInvalidExcessBlobGasBeforeCancun    = errors.New("invalid excessBlobGas before cancun")
	errInvalidBlobGasUsedBeforeCancun      = errors.New("invalid blobGasUsed before cancun")
	errInvalidParent                       = errors.New("parent header not found")
	errInvalidParentBeaconRootBeforeCancun = errors.New("invalid parentBeaconRoot before cancun")
	errInvalidExcessBlobGas                = errors.New("invalid excessBlobGas")
	errMissingParentBeaconRoot             = errors.New("header is missing parentBeaconRoot")
	errParentBeaconRootNonEmpty            = errors.New("invalid non-empty parentBeaconRoot")
	errBlobGasUsedNilInCancun              = errors.New("blob gas used must not be nil in Cancun")
	errBlobsNotEnabled                     = errors.New("blobs not enabled on avalanche networks")
)

// wrappedBlock implements the snowman.wrappedBlock interface
type wrappedBlock struct {
	id        ids.ID
	ethBlock  *types.Block
	extension extension.BlockExtension
	vm        *VM
}

// wrapBlock returns a new Block wrapping the ethBlock type and implementing the snowman.Block interface
func wrapBlock(ethBlock *types.Block, vm *VM) (*wrappedBlock, error) {
	b := &wrappedBlock{
		id:       ids.ID(ethBlock.Hash()),
		ethBlock: ethBlock,
		vm:       vm,
	}
	if vm.extensionConfig.BlockExtender != nil {
		extension, err := vm.extensionConfig.BlockExtender.NewBlockExtension(b)
		if err != nil {
			return nil, fmt.Errorf("failed to create block extension: %w", err)
		}
		b.extension = extension
	}
	return b, nil
}

// ID implements the snowman.Block interface
func (b *wrappedBlock) ID() ids.ID { return b.id }

// Accept implements the snowman.Block interface
func (b *wrappedBlock) Accept(context.Context) error {
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

	// Call Accept for relevant precompile logs before accepting on the chain.
	rules := b.vm.rules(b.ethBlock.Number(), b.ethBlock.Time())
	if err := b.handlePrecompileAccept(rules); err != nil {
		return err
	}
	if err := vm.blockChain.Accept(b.ethBlock); err != nil {
		return fmt.Errorf("chain could not accept %s: %w", blkID, err)
	}

	if err := vm.PutLastAcceptedID(blkID); err != nil {
		return fmt.Errorf("failed to put %s as the last accepted block: %w", blkID, err)
	}

	// No block extension batching path in subnet-evm; commit versioned DB directly
	return b.vm.versiondb.Commit()
}

// handlePrecompileAccept calls Accept on any logs generated with an active precompile address that implements
// contract.Accepter
func (b *wrappedBlock) handlePrecompileAccept(rules extras.Rules) error {
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
		Warp:    b.vm.warpMsgDB,
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
func (b *wrappedBlock) Reject(context.Context) error {
	blkID := b.ID()
	log.Debug("rejecting block",
		"hash", blkID.Hex(),
		"id", blkID,
		"height", b.Height(),
	)
	return b.vm.blockChain.Reject(b.ethBlock)
}

// Parent implements the snowman.Block interface
func (b *wrappedBlock) Parent() ids.ID {
	return ids.ID(b.ethBlock.ParentHash())
}

// Height implements the snowman.Block interface
func (b *wrappedBlock) Height() uint64 { return b.ethBlock.NumberU64() }

// Timestamp implements the snowman.Block interface
func (b *wrappedBlock) Timestamp() time.Time { return time.Unix(int64(b.ethBlock.Time()), 0) }

// Verify implements the snowman.Block interface
func (b *wrappedBlock) Verify(context.Context) error {
	return b.verify(&precompileconfig.PredicateContext{
		SnowCtx:            b.vm.ctx,
		ProposerVMBlockCtx: nil,
	}, true)
}

// ShouldVerifyWithContext implements the block.WithVerifyContext interface
func (b *wrappedBlock) ShouldVerifyWithContext(context.Context) (bool, error) {
	rules := b.vm.rules(b.ethBlock.Number(), b.ethBlock.Time())
	predicates := rules.Predicaters
	// Short circuit early if there are no predicates to verify
	if len(predicates) == 0 {
		return false, nil
	}

	// Check if any transaction specifies a precompile that enforces a predicate, requiring
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
func (b *wrappedBlock) VerifyWithContext(_ context.Context, proposerVMBlockCtx *block.Context) error {
	return b.verify(&precompileconfig.PredicateContext{
		SnowCtx:            b.vm.ctx,
		ProposerVMBlockCtx: proposerVMBlockCtx,
	}, true)
}

// Verify the block is valid.
// Enforces that the predicates are valid within [predicateContext].
// Writes the block details to disk and the state to the trie manager iff writes=true.
func (b *wrappedBlock) verify(predicateContext *precompileconfig.PredicateContext, writes bool) error {
	if predicateContext.ProposerVMBlockCtx != nil {
		log.Debug("Verifying block with context", "block", b.ID(), "height", b.Height())
	} else {
		log.Debug("Verifying block without context", "block", b.ID(), "height", b.Height())
	}

	if err := b.syntacticVerify(); err != nil {
		return fmt.Errorf("syntactic block verification failed: %w", err)
	}

	if err := b.semanticVerify(predicateContext); err != nil {
		return fmt.Errorf("failed to verify block: %w", err)
	}

	// The engine may call VerifyWithContext multiple times on the same block with different contexts.
	if b.vm.State.IsProcessing(b.id) {
		return nil
	}

	return b.vm.blockChain.InsertBlockManual(b.ethBlock, writes)
}

func (b *wrappedBlock) verifyIntrinsicGas() error {
	// Verify claimed gas used fits within available capacity for this header.
	// This checks that the gas used is less than the block's capacity.
	parentHash := b.ethBlock.ParentHash()
	parentHeight := b.ethBlock.NumberU64() - 1
	parent := b.vm.blockChain.GetHeader(parentHash, parentHeight)
	if parent == nil {
		return fmt.Errorf("%w: hash:%q height:%d",
			errMissingParentBlock,
			parentHash,
			parentHeight,
		)
	}

	// Verify that the claimed GasUsed is within the current capacity.
	feeConfig, _, err := b.vm.blockChain.GetFeeConfigAt(parent)
	if err != nil {
		return fmt.Errorf("failed to get fee config: %w", err)
	}
	if err := customheader.VerifyGasUsed(b.vm.chainConfigExtra(), feeConfig, parent, b.ethBlock.Header()); err != nil {
		return fmt.Errorf("%w: %w", errInvalidGasUsedRelativeToCapacity, err)
	}

	// Collect all intrinsic gas costs for all transactions in the block.
	rules := b.vm.chainConfig.Rules(b.ethBlock.Number(), params.IsMergeTODO, b.ethBlock.Time())
	var totalIntrinsicGasCost uint64
	for _, tx := range b.ethBlock.Transactions() {
		intrinsicGas, err := core.IntrinsicGas(tx.Data(), tx.AccessList(), tx.To() == nil, rules)
		if err != nil {
			return fmt.Errorf("failed to calculate intrinsic gas: %w for tx %s", err, tx.Hash())
		}

		totalIntrinsicGasCost, err = math.Add(totalIntrinsicGasCost, intrinsicGas)
		if err != nil {
			return fmt.Errorf("%w: intrinsic gas exceeds MaxUint64", errTotalIntrinsicGasCostExceedsClaimed)
		}
	}

	// Verify that the total intrinsic gas cost is less than or equal to the
	// claimed GasUsed.
	if claimedGasUsed := b.ethBlock.GasUsed(); totalIntrinsicGasCost > claimedGasUsed {
		return fmt.Errorf("%w: intrinsic gas (%d) > claimed gas used (%d)",
			errTotalIntrinsicGasCostExceedsClaimed,
			totalIntrinsicGasCost,
			claimedGasUsed,
		)
	}

	return nil
}

// semanticVerify verifies that a *Block is internally consistent.
func (b *wrappedBlock) semanticVerify(predicateContext *precompileconfig.PredicateContext) error {
	extraConfig := params.GetExtra(b.vm.chainConfig)
	parent := b.vm.blockChain.GetHeader(b.ethBlock.ParentHash(), b.ethBlock.NumberU64()-1)
	if parent == nil {
		return fmt.Errorf("%w: %s at height %d", errInvalidParent, b.ethBlock.ParentHash(), b.ethBlock.NumberU64()-1)
	}

	header := b.ethBlock.Header()
	// Ensure MinDelayExcess is consistent with rules and minimum block delay is enforced.
	if err := customheader.VerifyMinDelayExcess(extraConfig, parent, header); err != nil {
		return err
	}
	// Ensure Time and TimeMilliseconds are consistent with rules.
	if err := customheader.VerifyTime(extraConfig, parent, header, b.vm.clock.Time()); err != nil {
		return err
	}

	// If the VM is not marked as bootstrapped the other chains may also be
	// bootstrapping and not have populated the required indices. Since
	// bootstrapping only verifies blocks that have been canonically accepted by
	// the network, these checks would be guaranteed to pass on a synced node.
	if b.vm.bootstrapped.Get() {
		if err := b.verifyIntrinsicGas(); err != nil {
			return fmt.Errorf("failed to verify intrinsic gas: %w", err)
		}

		// Verify that all the ICM messages are correctly marked as either valid
		// or invalid.
		if err := b.verifyPredicates(predicateContext); err != nil {
			return fmt.Errorf("failed to verify predicates: %w", err)
		}
	}

	return nil
}

// syntacticVerify verifies that a *Block is well-formed.
func (b *wrappedBlock) syntacticVerify() error {
	if b == nil || b.ethBlock == nil {
		return errInvalidBlock
	}

	// Skip verification of the genesis block since it should already be marked as accepted.
	if b.ethBlock.Hash() == b.vm.genesisHash {
		return nil
	}

	ethHeader := b.ethBlock.Header()
	rules := b.vm.chainConfig.Rules(ethHeader.Number, params.IsMergeTODO, ethHeader.Time)
	rulesExtra := params.GetRulesExtra(rules)
	// Perform block and header sanity checks
	if !ethHeader.Number.IsUint64() {
		return fmt.Errorf("invalid block number: %v", ethHeader.Number)
	}
	if !ethHeader.Difficulty.IsUint64() || ethHeader.Difficulty.Cmp(common.Big1) != 0 {
		return fmt.Errorf("invalid difficulty: %d", ethHeader.Difficulty)
	}
	if ethHeader.Nonce.Uint64() != 0 {
		return fmt.Errorf(
			"expected nonce to be 0 but got %d: %w",
			ethHeader.Nonce.Uint64(), errInvalidNonce,
		)
	}

	if ethHeader.MixDigest != (common.Hash{}) {
		return fmt.Errorf("invalid mix digest: %v", ethHeader.MixDigest)
	}

	// Verify the extra data is well-formed.
	if err := customheader.VerifyExtra(rulesExtra.AvalancheRules, ethHeader.Extra); err != nil {
		return err
	}

	if rulesExtra.IsSubnetEVM {
		if ethHeader.BaseFee == nil {
			return errNilBaseFeeSubnetEVM
		}
		if bfLen := ethHeader.BaseFee.BitLen(); bfLen > 256 {
			return fmt.Errorf("too large base fee: bitlen %d", bfLen)
		}
	}

	// Check that the tx hash in the header matches the body
	txs := b.ethBlock.Transactions()
	if len(txs) == 0 {
		// Empty blocks are not allowed on Subnet-EVM
		return errEmptyBlock
	}
	txsHash := types.DeriveSha(txs, trie.NewStackTrie(nil))
	if txsHash != ethHeader.TxHash {
		return fmt.Errorf("invalid txs hash %v does not match calculated txs hash %v", ethHeader.TxHash, txsHash)
	}
	// Check that the uncle hash in the header matches the body
	uncleHash := types.CalcUncleHash(b.ethBlock.Uncles())
	if uncleHash != ethHeader.UncleHash {
		return fmt.Errorf("invalid uncle hash %v does not match calculated uncle hash %v", ethHeader.UncleHash, uncleHash)
	}

	// Block must not have any uncles
	if len(b.ethBlock.Uncles()) > 0 {
		return errUnclesUnsupported
	}

	if rulesExtra.IsSubnetEVM {
		blockGasCost := customtypes.GetHeaderExtra(ethHeader).BlockGasCost
		switch {
		// Make sure BlockGasCost is not nil
		// NOTE: ethHeader.BlockGasCost correctness is checked in header verification
		case blockGasCost == nil:
			return errNilBlockGasCostSubnetEVM
		case !blockGasCost.IsUint64():
			return fmt.Errorf("too large blockGasCost: %d", blockGasCost)
		}
	}

	// Verify the existence / non-existence of excessBlobGas
	if rules.IsCancun {
		switch {
		case ethHeader.ParentBeaconRoot == nil:
			return errMissingParentBeaconRoot
		case *ethHeader.ParentBeaconRoot != (common.Hash{}):
			return fmt.Errorf("%w: have %x, expected empty hash", errParentBeaconRootNonEmpty, ethHeader.ParentBeaconRoot)
		case ethHeader.BlobGasUsed == nil:
			return errBlobGasUsedNilInCancun
		case *ethHeader.BlobGasUsed != 0:
			return fmt.Errorf("%w: used %d blob gas, expected 0", errBlobsNotEnabled, *ethHeader.BlobGasUsed)
		case ethHeader.ExcessBlobGas == nil:
			return fmt.Errorf("%w: have nil, expected 0", errInvalidExcessBlobGas)
		case *ethHeader.ExcessBlobGas != 0:
			return fmt.Errorf("%w: have %d, expected 0", errInvalidExcessBlobGas, *ethHeader.ExcessBlobGas)
		}
	} else {
		switch {
		case ethHeader.ParentBeaconRoot != nil:
			return fmt.Errorf("%w: have %x, expected nil", errInvalidParentBeaconRootBeforeCancun, *ethHeader.ParentBeaconRoot)
		case ethHeader.ExcessBlobGas != nil:
			return fmt.Errorf("%w: have %d, expected nil", errInvalidExcessBlobGasBeforeCancun, *ethHeader.ExcessBlobGas)
		case ethHeader.BlobGasUsed != nil:
			return fmt.Errorf("%w: have %d, expected nil", errInvalidBlobGasUsedBeforeCancun, *ethHeader.BlobGasUsed)
		}
	}

	if b.extension != nil {
		if err := b.extension.SyntacticVerify(*rulesExtra); err != nil {
			return err
		}
	}
	return nil
}

// verifyPredicates verifies the predicates in the block are valid according to predicateContext.
func (b *wrappedBlock) verifyPredicates(predicateContext *precompileconfig.PredicateContext) error {
	rules := b.vm.chainConfig.Rules(b.ethBlock.Number(), params.IsMergeTODO, b.ethBlock.Time())
	rulesExtra := params.GetRulesExtra(rules)

	switch {
	case !rulesExtra.IsDurango && rulesExtra.PredicatersExist():
		return errors.New("cannot enable predicates before Durango activation")
	case !rulesExtra.IsDurango:
		return nil
	}

	predicateResults, err := core.CheckBlockPredicates(
		rules,
		predicateContext,
		b.ethBlock.Transactions(),
	)
	if err != nil {
		return err
	}
	predicateResultsBytes, err := predicateResults.Bytes()
	if err != nil {
		return fmt.Errorf("failed to marshal predicate results: %w", err)
	}
	extraData := b.ethBlock.Extra()
	headerPredicateResultsBytes := customheader.PredicateBytesFromExtra(extraData)
	if !bytes.Equal(headerPredicateResultsBytes, predicateResultsBytes) {
		return fmt.Errorf("%w (remote: %x local: %x)", errInvalidHeaderPredicateResults, headerPredicateResultsBytes, predicateResultsBytes)
	}
	return nil
}

// Bytes implements the snowman.Block interface
func (b *wrappedBlock) Bytes() []byte {
	res, err := rlp.EncodeToBytes(b.ethBlock)
	if err != nil {
		panic(err)
	}
	return res
}

func (b *wrappedBlock) String() string { return fmt.Sprintf("EVM block, ID = %s", b.ID()) }

func (b *wrappedBlock) GetEthBlock() *types.Block { return b.ethBlock }

func (b *wrappedBlock) GetBlockExtension() extension.BlockExtension { return b.extension }
