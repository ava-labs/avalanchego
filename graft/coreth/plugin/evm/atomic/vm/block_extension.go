// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/extension"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap5"
	"github.com/ava-labs/avalanchego/graft/coreth/utils"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var (
	_ extension.BlockExtension  = (*blockExtension)(nil)
	_ extension.BlockExtender   = (*blockExtender)(nil)
	_ atomic.AtomicBlockContext = (*blockExtension)(nil)
)

var (
	errNilEthBlock            = errors.New("nil ethBlock")
	ErrMissingUTXOs           = errors.New("missing UTXOs")
	ErrEmptyBlock             = errors.New("empty block")
	errAtomicExtractionFailed = errors.New("atomic tx extraction failed")
)

type blockExtender struct {
	extDataHashes map[common.Hash]common.Hash
	vm            *VM
}

type blockExtension struct {
	atomicTxs     []*atomic.Tx
	blockExtender *blockExtender
	block         extension.ExtendedBlock
}

// newBlockExtender returns a new block extender.
func newBlockExtender(
	extDataHashes map[common.Hash]common.Hash,
	vm *VM,
) *blockExtender {
	return &blockExtender{
		extDataHashes: extDataHashes,
		// Note: we need VM here to access the atomic backend that
		// could be initialized later in the VM.
		vm: vm,
	}
}

// NewBlockExtension returns a new block extension.
func (be *blockExtender) NewBlockExtension(b extension.ExtendedBlock) (extension.BlockExtension, error) {
	ethBlock := b.GetEthBlock()
	if ethBlock == nil {
		return nil, errNilEthBlock
	}
	// Extract atomic transactions from the block
	isApricotPhase5 := be.vm.chainConfigExtra().IsApricotPhase5(ethBlock.Time())
	atomicTxs, err := atomic.ExtractAtomicTxs(customtypes.BlockExtData(ethBlock), isApricotPhase5, atomic.Codec)
	if err != nil {
		return nil, errors.Join(errAtomicExtractionFailed, err)
	}

	return &blockExtension{
		atomicTxs:     atomicTxs,
		blockExtender: be,
		block:         b,
	}, nil
}

// SyntacticVerify checks the syntactic validity of the block. This is called by the wrapper
// block manager's SyntacticVerify method.
func (be *blockExtension) SyntacticVerify(rules extras.Rules) error {
	b := be.block
	ethBlock := b.GetEthBlock()
	blockExtender := be.blockExtender
	// should not happen
	if ethBlock == nil {
		return errNilEthBlock
	}
	ethHeader := ethBlock.Header()
	blockHash := ethBlock.Hash()
	headerExtra := customtypes.GetHeaderExtra(ethHeader)

	if !rules.IsApricotPhase1 {
		if blockExtender.extDataHashes != nil {
			extData := customtypes.BlockExtData(ethBlock)
			extDataHash := customtypes.CalcExtDataHash(extData)
			// If there is no extra data, check that there is no extra data in the hash map either to ensure we do not
			// have a block that is unexpectedly missing extra data.
			expectedExtDataHash, ok := blockExtender.extDataHashes[blockHash]
			if len(extData) == 0 {
				if ok {
					return fmt.Errorf("found block with unexpected missing extra data (%s, %d), expected extra data hash: %s", blockHash, b.Height(), expectedExtDataHash)
				}
			} else {
				// If there is extra data, check to make sure that the extra data hash matches the expected extra data hash for this
				// block
				if extDataHash != expectedExtDataHash {
					return fmt.Errorf("extra data hash in block (%s, %d): %s, did not match the expected extra data hash: %s", blockHash, b.Height(), extDataHash, expectedExtDataHash)
				}
			}
		}
	}

	// Verify the ExtDataHash field
	if rules.IsApricotPhase1 {
		extraData := customtypes.BlockExtData(ethBlock)
		hash := customtypes.CalcExtDataHash(extraData)
		if headerExtra.ExtDataHash != hash {
			return fmt.Errorf("extra data hash mismatch: have %x, want %x", headerExtra.ExtDataHash, hash)
		}
	} else if headerExtra.ExtDataHash != (common.Hash{}) {
		return fmt.Errorf(
			"expected ExtDataHash to be empty but got %x",
			headerExtra.ExtDataHash,
		)
	}

	// Block must not be empty
	txs := ethBlock.Transactions()
	atomicTxs := be.atomicTxs
	if len(txs) == 0 && len(atomicTxs) == 0 {
		return ErrEmptyBlock
	}

	// If we are in ApricotPhase4, ensure that ExtDataGasUsed is populated correctly.
	if rules.IsApricotPhase4 {
		// After the F upgrade, the extDataGasUsed field is validated by
		// [header.VerifyGasUsed].
		if !rules.IsFortuna && rules.IsApricotPhase5 {
			if !utils.BigLessOrEqualUint64(headerExtra.ExtDataGasUsed, ap5.AtomicGasLimit) {
				return fmt.Errorf("too large extDataGasUsed: %d", headerExtra.ExtDataGasUsed)
			}
		}
		var totalGasUsed uint64
		for _, atomicTx := range atomicTxs {
			// We perform this check manually here to avoid the overhead of having to
			// reparse the atomicTx in `CalcExtDataGasUsed`.
			fixedFee := rules.IsApricotPhase5 // Charge the atomic tx fixed fee as of ApricotPhase5
			gasUsed, err := atomicTx.GasUsed(fixedFee)
			if err != nil {
				return err
			}
			totalGasUsed, err = safemath.Add(totalGasUsed, gasUsed)
			if err != nil {
				return err
			}
		}

		if !utils.BigEqualUint64(headerExtra.ExtDataGasUsed, totalGasUsed) {
			return fmt.Errorf("invalid extDataGasUsed: have %d, want %d", headerExtra.ExtDataGasUsed, totalGasUsed)
		}
	}

	return nil
}

// SemanticVerify checks the semantic validity of the block. This is called by the wrapper
// block manager's SemanticVerify method.
func (be *blockExtension) SemanticVerify() error {
	vm := be.blockExtender.vm
	if vm.bootstrapped.Get() {
		// Verify that the UTXOs named in import txs are present in shared
		// memory.
		//
		// This does not fully verify that this block can spend these UTXOs.
		// However, it guarantees that any block that fails the later checks was
		// built by an incorrect block proposer. This ensures that we only mark
		// blocks as BAD BLOCKs if they were incorrectly generated.
		if err := be.verifyUTXOsPresent(be.atomicTxs); err != nil {
			return err
		}
	}
	return nil
}

// Accept is called when the block is accepted. This is called by the wrapper
// block manager's Accept method. The acceptedBatch contains the changes that
// were made to the database as a result of accepting the block, and it's flushed
// to the database in this method.
func (be *blockExtension) Accept(acceptedBatch database.Batch) error {
	vm := be.blockExtender.vm
	for _, tx := range be.atomicTxs {
		// Remove the accepted transaction from the mempool
		vm.AtomicMempool.RemoveTx(tx)
	}

	// Update VM state for atomic txs in this block. This includes updating the
	// atomic tx repo, atomic trie, and shared memory.
	atomicState, err := vm.AtomicBackend.GetVerifiedAtomicState(common.Hash(be.block.ID()))
	if err != nil {
		// should never occur since [b] must be verified before calling Accept
		return err
	}
	// Apply any shared memory changes atomically with other pending batched changes
	return atomicState.Accept(acceptedBatch)
}

// Reject is called when the block is rejected. This is called by the wrapper
// block manager's Reject method.
func (be *blockExtension) Reject() error {
	vm := be.blockExtender.vm
	for _, tx := range be.atomicTxs {
		// Re-issue the transaction in the mempool, continue even if it fails
		vm.AtomicMempool.RemoveTx(tx)
		if err := vm.AtomicMempool.AddRemoteTx(tx); err != nil {
			log.Debug("Failed to re-issue transaction in rejected block", "txID", tx.ID(), "err", err)
		}
	}
	atomicState, err := vm.AtomicBackend.GetVerifiedAtomicState(common.Hash(be.block.ID()))
	if err != nil {
		// should never occur since [b] must be verified before calling Reject
		return err
	}
	return atomicState.Reject()
}

// CleanupVerified is called when the block is cleaned up after a failed insertion.
func (be *blockExtension) CleanupVerified() {
	vm := be.blockExtender.vm
	// If the state isn't found, no need to reject (it was never verified).
	atomicState, err := vm.AtomicBackend.GetVerifiedAtomicState(be.block.GetEthBlock().Hash())
	if err != nil {
		return
	}
	// atomicState.Reject() never returns an error in practice.
	atomicState.Reject() //nolint:errcheck // Reject never returns an error
}

// AtomicTxs returns the atomic transactions in this block.
func (be *blockExtension) AtomicTxs() []*atomic.Tx {
	return be.atomicTxs
}

// verifyUTXOsPresent verifies all atomic UTXOs consumed by the block are
// present in shared memory.
func (be *blockExtension) verifyUTXOsPresent(atomicTxs []*atomic.Tx) error {
	b := be.block
	blockHash := common.Hash(b.ID())
	vm := be.blockExtender.vm
	if vm.AtomicBackend.IsBonus(b.Height(), blockHash) {
		log.Info("skipping atomic tx verification on bonus block", "block", blockHash)
		return nil
	}

	// verify UTXOs named in import txs are present in shared memory.
	for _, atomicTx := range atomicTxs {
		utx := atomicTx.UnsignedAtomicTx
		chainID, requests, err := utx.AtomicOps()
		if err != nil {
			return err
		}
		if _, err := vm.Ctx.SharedMemory.Get(chainID, requests.RemoveRequests); err != nil {
			return fmt.Errorf("%w: %w", ErrMissingUTXOs, err)
		}
	}
	return nil
}
