// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"

	coreth "github.com/ava-labs/coreth/chain"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/trie"
)

var (
	phase0BlockValidator = blockValidatorPhase0{}
	phase1BlockValidator = blockValidatorPhase1{}
)

type BlockValidator interface {
	SyntacticVerify(b *Block) error
}

type blockValidatorPhase0 struct {
	extDataHashes map[common.Hash]common.Hash
}

func (v blockValidatorPhase0) SyntacticVerify(b *Block) error {
	if b == nil || b.ethBlock == nil {
		return errInvalidBlock
	}

	blockHash := b.ethBlock.Hash()
	if v.extDataHashes != nil {
		extData := b.ethBlock.ExtData()
		extDataHash := types.CalcExtDataHash(extData)
		// If there is no extra data, check that there is no extra data in the hash map either to ensure we do not
		// have a block that is unexpectedly missing extra data.
		expectedExtDataHash, ok := v.extDataHashes[blockHash]
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

	// Skip verification of the genesis block since it
	// should already be marked as accepted
	if blockHash == b.vm.genesisHash {
		return nil
	}

	// Perform block and header sanity checks
	ethHeader := b.ethBlock.Header()
	if ethHeader.Number == nil || !ethHeader.Number.IsUint64() {
		return errInvalidBlock
	}
	if ethHeader.Difficulty == nil || !ethHeader.Difficulty.IsUint64() ||
		ethHeader.Difficulty.Uint64() != 1 {
		return fmt.Errorf(
			"expected difficulty to be 1 but got %v: %w",
			ethHeader.Difficulty, errInvalidDifficulty,
		)
	}
	if ethHeader.Nonce.Uint64() != 0 {
		return fmt.Errorf(
			"expected nonce to be 0 but got %d: %w",
			ethHeader.Nonce.Uint64(), errInvalidNonce,
		)
	}
	if ethHeader.MixDigest != (common.Hash{}) {
		return fmt.Errorf(
			"expected MixDigest to be empty but got %x: %w",
			ethHeader.MixDigest, errInvalidMixDigest,
		)
	}
	if ethHeader.ExtDataHash != (common.Hash{}) {
		return fmt.Errorf(
			"expected ExtDataHash to be empty but got %x: %w",
			ethHeader.ExtDataHash, errInvalidExtDataHash,
		)
	}
	headerExtraDataSize := uint64(len(ethHeader.Extra))
	if headerExtraDataSize > params.MaximumExtraDataSize {
		return fmt.Errorf(
			"expected header ExtraData to be <= %d but got %d: %w",
			params.MaximumExtraDataSize, headerExtraDataSize, errHeaderExtraDataTooBig,
		)
	}
	if b.ethBlock.Version() != 0 {
		return fmt.Errorf(
			"expected block version to be 0 but got %d: %w",
			b.ethBlock.Version(), errInvalidBlockVersion,
		)
	}

	// Check that the tx hash in the header matches the body
	txsHash := types.DeriveSha(b.ethBlock.Transactions(), new(trie.Trie))
	if txsHash != ethHeader.TxHash {
		return errTxHashMismatch
	}
	// Check that the uncle hash in the header matches the body
	uncleHash := types.CalcUncleHash(b.ethBlock.Uncles())
	if uncleHash != ethHeader.UncleHash {
		return errUncleHashMismatch
	}
	// Coinbase must be zero on C-Chain
	if b.ethBlock.Coinbase() != coreth.BlackholeAddr {
		return errInvalidBlock
	}
	// Block must not have any uncles
	if len(b.ethBlock.Uncles()) > 0 {
		return errUnclesUnsupported
	}
	// Block must not be empty
	//
	// Note: getAtomicTx also asserts a maximum size
	atomicTx, err := b.vm.getAtomicTx(b.ethBlock)
	if err != nil {
		return err
	}
	txs := b.ethBlock.Transactions()
	if len(txs) == 0 && atomicTx == nil {
		return errEmptyBlock
	}

	// Make sure that all the txs have the correct fee set.
	for _, tx := range txs {
		if tx.GasPrice().Cmp(params.LaunchMinGasPrice) < 0 {
			return fmt.Errorf("block contains tx %s with gas price too low (%d < %d)", tx.Hash(), tx.GasPrice(), params.LaunchMinGasPrice)
		}
	}

	// Make sure the block isn't too far in the future
	blockTimestamp := b.ethBlock.Time()
	if maxBlockTime := uint64(b.vm.clock.Time().Add(maxFutureBlockTime).Unix()); blockTimestamp > maxBlockTime {
		return fmt.Errorf("block timestamp is too far in the future: %d > allowed %d", blockTimestamp, maxBlockTime)
	}
	return nil
}

type blockValidatorPhase1 struct{}

func (blockValidatorPhase1) SyntacticVerify(b *Block) error {
	if b == nil || b.ethBlock == nil {
		return errInvalidBlock
	}

	// Skip verification of the genesis block since it
	// should already be marked as accepted
	if b.ethBlock.Hash() == b.vm.genesisHash {
		return nil
	}

	// Perform block and header sanity checks
	ethHeader := b.ethBlock.Header()
	if ethHeader.Number == nil || !ethHeader.Number.IsUint64() {
		return errInvalidBlock
	}
	if ethHeader.Difficulty == nil || !ethHeader.Difficulty.IsUint64() ||
		ethHeader.Difficulty.Uint64() != 1 {
		return fmt.Errorf(
			"expected difficulty to be 1 but got %v: %w",
			ethHeader.Difficulty, errInvalidDifficulty,
		)
	}
	if ethHeader.Nonce.Uint64() != 0 {
		return fmt.Errorf(
			"expected nonce to be 0 but got %d: %w",
			ethHeader.Nonce.Uint64(), errInvalidNonce,
		)
	}
	if ethHeader.GasLimit != params.ApricotPhase1GasLimit {
		return fmt.Errorf(
			"expected gas limit to be %d in apricot phase 1 but got %d",
			params.ApricotPhase1GasLimit, ethHeader.GasLimit,
		)
	}
	if ethHeader.MixDigest != (common.Hash{}) {
		return fmt.Errorf(
			"expected MixDigest to be empty but got %x: %w",
			ethHeader.MixDigest, errInvalidMixDigest,
		)
	}
	if hash := types.CalcExtDataHash(b.ethBlock.ExtData()); ethHeader.ExtDataHash != hash {
		return fmt.Errorf("extra data hash mismatch: have %x, want %x", ethHeader.ExtDataHash, hash)
	}
	headerExtraDataSize := uint64(len(ethHeader.Extra))
	if headerExtraDataSize > 0 {
		return fmt.Errorf(
			"expected header ExtraData to be <= 0 but got %d: %w",
			headerExtraDataSize, errHeaderExtraDataTooBig,
		)
	}
	if b.ethBlock.Version() != 0 {
		return fmt.Errorf(
			"expected block version to be 0 but got %d: %w",
			b.ethBlock.Version(), errInvalidBlockVersion,
		)
	}

	// Check that the tx hash in the header matches the body
	txsHash := types.DeriveSha(b.ethBlock.Transactions(), new(trie.Trie))
	if txsHash != ethHeader.TxHash {
		return errTxHashMismatch
	}
	// Check that the uncle hash in the header matches the body
	uncleHash := types.CalcUncleHash(b.ethBlock.Uncles())
	if uncleHash != ethHeader.UncleHash {
		return errUncleHashMismatch
	}
	// Coinbase must be zero on C-Chain
	if b.ethBlock.Coinbase() != coreth.BlackholeAddr {
		return errInvalidBlock
	}
	// Block must not have any uncles
	if len(b.ethBlock.Uncles()) > 0 {
		return errUnclesUnsupported
	}
	// Block must not be empty
	//
	// Note: getAtomicTx also asserts a maximum size
	atomicTx, err := b.vm.getAtomicTx(b.ethBlock)
	if err != nil {
		return err
	}
	txs := b.ethBlock.Transactions()
	if len(txs) == 0 && atomicTx == nil {
		return errEmptyBlock
	}

	// Make sure that all the txs have the correct fee set.
	for _, tx := range txs {
		if tx.GasPrice().Cmp(params.ApricotPhase1MinGasPrice) < 0 {
			return fmt.Errorf("block contains tx %s with gas price too low (%d < %d)", tx.Hash(), tx.GasPrice(), params.ApricotPhase1MinGasPrice)
		}
	}

	// Make sure the block isn't too far in the future
	blockTimestamp := b.ethBlock.Time()
	if maxBlockTime := uint64(b.vm.clock.Time().Add(maxFutureBlockTime).Unix()); blockTimestamp > maxBlockTime {
		return fmt.Errorf("block timestamp is too far in the future: %d > allowed %d", blockTimestamp, maxBlockTime)
	}
	return nil
}
