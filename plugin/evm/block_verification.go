// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/trie"
)

var legacyMinGasPrice = big.NewInt(params.MinGasPrice)

type BlockValidator interface {
	SyntacticVerify(b *Block, rules params.Rules) error
}

type blockValidator struct{}

func NewBlockValidator() BlockValidator {
	return &blockValidator{}
}

func (v blockValidator) SyntacticVerify(b *Block, rules params.Rules) error {
	if b == nil || b.ethBlock == nil {
		return errInvalidBlock
	}
	ethHeader := b.ethBlock.Header()
	blockHash := b.ethBlock.Hash()

	// Skip verification of the genesis block since it should already be marked as accepted.
	if blockHash == b.vm.genesisHash {
		return nil
	}

	// Perform block and header sanity checks
	if ethHeader.Number == nil || !ethHeader.Number.IsUint64() {
		return errInvalidBlock
	}
	if ethHeader.Difficulty == nil || !ethHeader.Difficulty.IsUint64() ||
		ethHeader.Difficulty.Uint64() != 1 {
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

	// Check that the size of the header's Extra data field is correct for [rules].
	headerExtraDataSize := len(ethHeader.Extra)
	switch {
	case rules.IsDurango:
		if headerExtraDataSize < params.DynamicFeeExtraDataSize {
			return fmt.Errorf(
				"expected header ExtraData to be len >= %d but got %d",
				params.DynamicFeeExtraDataSize, len(ethHeader.Extra),
			)
		}
	case rules.IsSubnetEVM:
		if headerExtraDataSize != params.DynamicFeeExtraDataSize {
			return fmt.Errorf(
				"expected header ExtraData to be len %d but got %d",
				params.DynamicFeeExtraDataSize, headerExtraDataSize,
			)
		}
	default:
		if uint64(headerExtraDataSize) > params.MaximumExtraDataSize {
			return fmt.Errorf(
				"expected header ExtraData to be <= %d but got %d",
				params.MaximumExtraDataSize, headerExtraDataSize,
			)
		}
	}

	if rules.IsSubnetEVM {
		if ethHeader.BaseFee == nil {
			return errNilBaseFeeSubnetEVM
		}
		if bfLen := ethHeader.BaseFee.BitLen(); bfLen > 256 {
			return fmt.Errorf("too large base fee: bitlen %d", bfLen)
		}
	}

	// Check that the tx hash in the header matches the body
	txsHash := types.DeriveSha(b.ethBlock.Transactions(), trie.NewStackTrie(nil))
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

	// Block must not be empty
	txs := b.ethBlock.Transactions()
	if len(txs) == 0 {
		return errEmptyBlock
	}

	if !rules.IsSubnetEVM {
		// Make sure that all the txs have the correct fee set.
		for _, tx := range txs {
			if tx.GasPrice().Cmp(legacyMinGasPrice) < 0 {
				return fmt.Errorf("block contains tx %s with gas price too low (%d < %d)", tx.Hash(), tx.GasPrice(), legacyMinGasPrice)
			}
		}
	}

	// Make sure the block isn't too far in the future
	blockTimestamp := b.ethBlock.Time()
	if maxBlockTime := uint64(b.vm.clock.Time().Add(maxFutureBlockTime).Unix()); blockTimestamp > maxBlockTime {
		return fmt.Errorf("block timestamp is too far in the future: %d > allowed %d", blockTimestamp, maxBlockTime)
	}

	if rules.IsSubnetEVM {
		switch {
		// Make sure BlockGasCost is not nil
		// NOTE: ethHeader.BlockGasCost correctness is checked in header verification
		case ethHeader.BlockGasCost == nil:
			return errNilBlockGasCostSubnetEVM
		case !ethHeader.BlockGasCost.IsUint64():
			return fmt.Errorf("too large blockGasCost: %d", ethHeader.BlockGasCost)
		}
	}

	// Verify the existence / non-existence of excessDataGas
	if rules.IsCancun && ethHeader.ExcessDataGas == nil {
		return errors.New("missing excessDataGas")
	}
	if !rules.IsCancun && ethHeader.ExcessDataGas != nil {
		return fmt.Errorf("invalid excessDataGas: have %d, expected nil", ethHeader.ExcessDataGas)
	}

	return nil
}
