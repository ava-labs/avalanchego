// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"errors"
	"fmt"

	"github.com/ava-labs/coreth/consensus"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap0"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	ethparams "github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/trie"
)

// BlockValidator is responsible for validating block headers, uncles and
// processed state.
//
// BlockValidator implements Validator.
type BlockValidator struct {
	config *params.ChainConfig // Chain configuration options
	bc     *BlockChain         // Canonical block chain
	engine consensus.Engine    // Consensus engine used for validating
}

// NewBlockValidator returns a new block validator which is safe for re-use
func NewBlockValidator(config *params.ChainConfig, blockchain *BlockChain, engine consensus.Engine) *BlockValidator {
	validator := &BlockValidator{
		config: config,
		engine: engine,
		bc:     blockchain,
	}
	return validator
}

// ValidateBody validates the given block's uncles and verifies the block
// header's transaction and uncle roots. The headers are assumed to be already
// validated at this point.
func (v *BlockValidator) ValidateBody(block *types.Block) error {
	// Check whether the block is already imported.
	if v.bc.HasBlockAndState(block.Hash(), block.NumberU64()) {
		return ErrKnownBlock
	}

	// Header validity is known at this point. Here we verify that uncle and transactions
	// given in the block body match the header.
	header := block.Header()
	if err := v.engine.VerifyUncles(v.bc, block); err != nil {
		return err
	}
	if hash := types.CalcUncleHash(block.Uncles()); hash != header.UncleHash {
		return fmt.Errorf("uncle root hash mismatch (header value %x, calculated %x)", header.UncleHash, hash)
	}
	if hash := types.DeriveSha(block.Transactions(), trie.NewStackTrie(nil)); hash != header.TxHash {
		return fmt.Errorf("transaction root hash mismatch (header value %x, calculated %x)", header.TxHash, hash)
	}

	// Blob transactions may be present after the Cancun fork.
	var blobs int
	for i, tx := range block.Transactions() {
		// Count the number of blobs to validate against the header's blobGasUsed
		blobs += len(tx.BlobHashes())

		// If the tx is a blob tx, it must NOT have a sidecar attached to be valid in a block.
		if tx.BlobTxSidecar() != nil {
			return fmt.Errorf("unexpected blob sidecar in transaction at index %d", i)
		}

		// The individual checks for blob validity (version-check + not empty)
		// happens in StateTransition.
	}

	// Check blob gas usage.
	if header.BlobGasUsed != nil {
		if want := *header.BlobGasUsed / ethparams.BlobTxBlobGasPerBlob; uint64(blobs) != want { // div because the header is surely good vs the body might be bloated
			return fmt.Errorf("blob gas used mismatch (header %v, calculated %v)", *header.BlobGasUsed, blobs*ethparams.BlobTxBlobGasPerBlob)
		}
	} else {
		if blobs > 0 {
			return errors.New("data blobs present in block body")
		}
	}

	// Ancestor block must be known.
	if !v.bc.HasBlockAndState(block.ParentHash(), block.NumberU64()-1) {
		if !v.bc.HasBlock(block.ParentHash(), block.NumberU64()-1) {
			return consensus.ErrUnknownAncestor
		}
		return consensus.ErrPrunedAncestor
	}
	return nil
}

// ValidateState validates the various changes that happen after a state transition,
// such as amount of used gas, the receipt roots and the state root itself.
func (v *BlockValidator) ValidateState(block *types.Block, statedb *state.StateDB, receipts types.Receipts, usedGas uint64) error {
	header := block.Header()
	if block.GasUsed() != usedGas {
		return fmt.Errorf("invalid gas used (remote: %d local: %d)", block.GasUsed(), usedGas)
	}
	// Validate the received block's bloom with the one derived from the generated receipts.
	// For valid blocks this should always validate to true.
	rbloom := types.CreateBloom(receipts)
	if rbloom != header.Bloom {
		return fmt.Errorf("invalid bloom (remote: %x  local: %x)", header.Bloom, rbloom)
	}
	// Tre receipt Trie's root (R = (Tr [[H1, R1], ... [Hn, Rn]]))
	receiptSha := types.DeriveSha(receipts, trie.NewStackTrie(nil))
	if receiptSha != header.ReceiptHash {
		return fmt.Errorf("invalid receipt root hash (remote: %x local: %x)", header.ReceiptHash, receiptSha)
	}
	// Validate the state root against the received state root and throw
	// an error if they don't match.
	if root := statedb.IntermediateRoot(v.config.IsEIP158(header.Number)); header.Root != root {
		return fmt.Errorf("invalid merkle root (remote: %x local: %x) dberr: %w", header.Root, root, statedb.Error())
	}
	return nil
}

// CalcGasLimit computes the gas limit of the next block after parent. It aims
// to keep the baseline gas above the provided floor, and increase it towards the
// ceil if the blocks are full. If the ceil is exceeded, it will always decrease
// the gas allowance.
func CalcGasLimit(parentGasUsed, parentGasLimit, gasFloor, gasCeil uint64) uint64 {
	// contrib = (parentGasUsed * 3 / 2) / 1024
	contrib := (parentGasUsed + parentGasUsed/2) / ethparams.GasLimitBoundDivisor

	// decay = parentGasLimit / 1024 -1
	decay := parentGasLimit/ethparams.GasLimitBoundDivisor - 1

	/*
		strategy: gasLimit of block-to-mine is set based on parent's
		gasUsed value.  if parentGasUsed > parentGasLimit * (2/3) then we
		increase it, otherwise lower it (or leave it unchanged if it's right
		at that usage) the amount increased/decreased depends on how far away
		from parentGasLimit * (2/3) parentGasUsed is.
	*/
	limit := parentGasLimit - decay + contrib
	if limit < ap0.MinGasLimit {
		limit = ap0.MinGasLimit
	}
	// If we're outside our allowed gas range, we try to hone towards them
	if limit < gasFloor {
		limit = parentGasLimit + decay
		if limit > gasFloor {
			limit = gasFloor
		}
	} else if limit > gasCeil {
		limit = parentGasLimit - decay
		if limit < gasCeil {
			limit = gasCeil
		}
	}
	return limit
}
