// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/trie"

	subnetEVM "github.com/ava-labs/subnet-evm/chain"
)

var (
	legacyBlockValidator    = blockValidatorLegacy{}
	legacyMinGasPrice       = big.NewInt(params.MinGasPrice)
	subnetEVMBlockValidator = blockValidatorSubnetEVM{}
)

type BlockValidator interface {
	SyntacticVerify(b *Block) error
}

type blockValidatorLegacy struct{}

func (v blockValidatorLegacy) SyntacticVerify(b *Block) error {
	if b == nil || b.ethBlock == nil {
		return errInvalidBlock
	}

	blockHash := b.ethBlock.Hash()

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
	headerExtraDataSize := uint64(len(ethHeader.Extra))
	if headerExtraDataSize > params.MaximumExtraDataSize {
		return fmt.Errorf(
			"expected header ExtraData to be <= %d but got %d: %w",
			params.MaximumExtraDataSize, headerExtraDataSize, errHeaderExtraDataTooBig,
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
	if b.ethBlock.Coinbase() != subnetEVM.BlackholeAddr {
		return errInvalidBlock
	}
	// Block must not have any uncles
	if len(b.ethBlock.Uncles()) > 0 {
		return errUnclesUnsupported
	}
	// Block must not be empty
	txs := b.ethBlock.Transactions()
	if len(txs) == 0 {
		return errEmptyBlock
	}

	// Make sure that all the txs have the correct fee set.
	for _, tx := range txs {
		if tx.GasPrice().Cmp(legacyMinGasPrice) < 0 {
			return fmt.Errorf("block contains tx %s with gas price too low (%d < %d)", tx.Hash(), tx.GasPrice(), legacyMinGasPrice)
		}
	}

	// Make sure the block isn't too far in the future
	blockTimestamp := b.ethBlock.Time()
	if maxBlockTime := uint64(b.vm.clock.Time().Add(maxFutureBlockTime).Unix()); blockTimestamp > maxBlockTime {
		return fmt.Errorf("block timestamp is too far in the future: %d > allowed %d", blockTimestamp, maxBlockTime)
	}
	return nil
}

type blockValidatorSubnetEVM struct{}

func (blockValidatorSubnetEVM) SyntacticVerify(b *Block) error {
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
	expectedGas := b.vm.chainConfig.GetFeeConfig().GasLimit.Uint64()
	if ethHeader.GasLimit != expectedGas {
		return fmt.Errorf(
			"expected gas limit to be %d in subnetEVM but got %d",
			expectedGas, ethHeader.GasLimit,
		)
	}
	if ethHeader.MixDigest != (common.Hash{}) {
		return fmt.Errorf(
			"expected MixDigest to be empty but got %x: %w",
			ethHeader.MixDigest, errInvalidMixDigest,
		)
	}

	expectedExtraDataSize := params.ExtraDataSize
	if headerExtraDataSize := len(ethHeader.Extra); headerExtraDataSize != expectedExtraDataSize {
		return fmt.Errorf(
			"expected header ExtraData to be %d but got %d: %w",
			expectedExtraDataSize, headerExtraDataSize, errHeaderExtraDataTooBig,
		)
	}
	if ethHeader.BaseFee == nil {
		return errNilBaseFeeSubnetEVM
	}
	if bfLen := ethHeader.BaseFee.BitLen(); bfLen > 256 {
		return fmt.Errorf("too large base fee: bitlen %d", bfLen)
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
	// Coinbase must be zero, if AllowFeeRecipients is not enabled
	if !b.vm.chainConfig.AllowFeeRecipients && b.ethBlock.Coinbase() != subnetEVM.BlackholeAddr {
		return errInvalidBlock
	}
	// Block must not have any uncles
	if len(b.ethBlock.Uncles()) > 0 {
		return errUnclesUnsupported
	}
	// Block must not be empty
	txs := b.ethBlock.Transactions()
	if len(txs) == 0 {
		return errEmptyBlock
	}

	// Make sure the block isn't too far in the future
	blockTimestamp := b.ethBlock.Time()
	if maxBlockTime := uint64(b.vm.clock.Time().Add(maxFutureBlockTime).Unix()); blockTimestamp > maxBlockTime {
		return fmt.Errorf("block timestamp is too far in the future: %d > allowed %d", blockTimestamp, maxBlockTime)
	}

	switch {
	// Make sure BlockGasCost is not nil
	// NOTE: ethHeader.BlockGasCost correctness is checked in header verification
	case ethHeader.BlockGasCost == nil:
		return errNilBlockGasCostSubnetEVM
	case !ethHeader.BlockGasCost.IsUint64():
		return fmt.Errorf("too large blockGasCost: %d", ethHeader.BlockGasCost)
	}
	return nil
}
