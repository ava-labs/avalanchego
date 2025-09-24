// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/trie"

	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/plugin/evm/customheader"
	"github.com/ava-labs/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/subnet-evm/plugin/evm/upgrade/legacy"
)

var legacyMinGasPrice = big.NewInt(legacy.BaseFee)

type BlockValidator interface {
	SyntacticVerify(b *Block, rules params.Rules) error
}

type blockValidator struct{}

func NewBlockValidator() BlockValidator {
	return &blockValidator{}
}

func (blockValidator) SyntacticVerify(b *Block, rules params.Rules) error {
	rulesExtra := params.GetRulesExtra(rules)
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

	if !rulesExtra.IsSubnetEVM {
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
	cancun := rules.IsCancun
	if !cancun && ethHeader.ExcessBlobGas != nil {
		return fmt.Errorf("invalid excessBlobGas: have %d, expected nil", *ethHeader.ExcessBlobGas)
	}
	if !cancun && ethHeader.BlobGasUsed != nil {
		return fmt.Errorf("invalid blobGasUsed: have %d, expected nil", *ethHeader.BlobGasUsed)
	}
	if cancun && ethHeader.ExcessBlobGas == nil {
		return errors.New("header is missing excessBlobGas")
	}
	if cancun && ethHeader.BlobGasUsed == nil {
		return errors.New("header is missing blobGasUsed")
	}
	if !cancun && ethHeader.ParentBeaconRoot != nil {
		return fmt.Errorf("invalid parentBeaconRoot: have %x, expected nil", *ethHeader.ParentBeaconRoot)
	}
	// TODO: decide what to do after Cancun
	// currently we are enforcing it to be empty hash
	if cancun {
		switch {
		case ethHeader.ParentBeaconRoot == nil:
			return errors.New("header is missing parentBeaconRoot")
		case *ethHeader.ParentBeaconRoot != (common.Hash{}):
			return fmt.Errorf("invalid parentBeaconRoot: have %x, expected empty hash", ethHeader.ParentBeaconRoot)
		}
	}
	return nil
}
