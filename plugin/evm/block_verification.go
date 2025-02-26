// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	safemath "github.com/ava-labs/avalanchego/utils/math"

	"github.com/ava-labs/coreth/constants"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/header"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap0"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap1"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap5"
	"github.com/ava-labs/coreth/trie"
	"github.com/ava-labs/coreth/utils"
)

var (
	ap0MinGasPrice = big.NewInt(ap0.MinGasPrice)
	ap1MinGasPrice = big.NewInt(ap1.MinGasPrice)
)

type BlockValidator interface {
	SyntacticVerify(b *Block, rules params.Rules) error
}

type blockValidator struct {
	extDataHashes map[common.Hash]common.Hash
}

func NewBlockValidator(extDataHashes map[common.Hash]common.Hash) BlockValidator {
	return &blockValidator{
		extDataHashes: extDataHashes,
	}
}

func (v blockValidator) SyntacticVerify(b *Block, rules params.Rules) error {
	if b == nil || b.ethBlock == nil {
		return errInvalidBlock
	}

	ethHeader := b.ethBlock.Header()
	blockHash := b.ethBlock.Hash()

	if !rules.IsApricotPhase1 {
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
	}

	// Skip verification of the genesis block since it should already be marked as accepted.
	if blockHash == b.vm.genesisHash {
		return nil
	}

	// Verify the ExtDataHash field
	if rules.IsApricotPhase1 {
		if hash := types.CalcExtDataHash(b.ethBlock.ExtData()); ethHeader.ExtDataHash != hash {
			return fmt.Errorf("extra data hash mismatch: have %x, want %x", ethHeader.ExtDataHash, hash)
		}
	} else {
		if ethHeader.ExtDataHash != (common.Hash{}) {
			return fmt.Errorf(
				"expected ExtDataHash to be empty but got %x",
				ethHeader.ExtDataHash,
			)
		}
	}

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
	if err := header.VerifyExtra(rules.AvalancheRules, ethHeader.Extra); err != nil {
		return err
	}

	if b.ethBlock.Version() != 0 {
		return fmt.Errorf("invalid version: %d", b.ethBlock.Version())
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
	// Coinbase must match the BlackholeAddr on C-Chain
	if ethHeader.Coinbase != constants.BlackholeAddr {
		return fmt.Errorf("invalid coinbase %v does not match required blackhole address %v", ethHeader.Coinbase, constants.BlackholeAddr)
	}
	// Block must not have any uncles
	if len(b.ethBlock.Uncles()) > 0 {
		return errUnclesUnsupported
	}

	// Block must not be empty
	txs := b.ethBlock.Transactions()
	if len(txs) == 0 && len(b.atomicTxs) == 0 {
		return errEmptyBlock
	}

	// Enforce minimum gas prices here prior to dynamic fees going into effect.
	switch {
	case !rules.IsApricotPhase1:
		// If we are in ApricotPhase0, enforce each transaction has a minimum gas price of at least the LaunchMinGasPrice
		for _, tx := range b.ethBlock.Transactions() {
			if tx.GasPrice().Cmp(ap0MinGasPrice) < 0 {
				return fmt.Errorf("block contains tx %s with gas price too low (%d < %d)", tx.Hash(), tx.GasPrice(), ap0.MinGasPrice)
			}
		}
	case !rules.IsApricotPhase3:
		// If we are prior to ApricotPhase3, enforce each transaction has a minimum gas price of at least the ApricotPhase1MinGasPrice
		for _, tx := range b.ethBlock.Transactions() {
			if tx.GasPrice().Cmp(ap1MinGasPrice) < 0 {
				return fmt.Errorf("block contains tx %s with gas price too low (%d < %d)", tx.Hash(), tx.GasPrice(), ap1.MinGasPrice)
			}
		}
	}

	// Make sure the block isn't too far in the future
	// TODO: move this to only be part of semantic verification.
	blockTimestamp := b.ethBlock.Time()
	if maxBlockTime := uint64(b.vm.clock.Time().Add(maxFutureBlockTime).Unix()); blockTimestamp > maxBlockTime {
		return fmt.Errorf("block timestamp is too far in the future: %d > allowed %d", blockTimestamp, maxBlockTime)
	}

	// Ensure BaseFee is non-nil as of ApricotPhase3.
	if rules.IsApricotPhase3 {
		if ethHeader.BaseFee == nil {
			return errNilBaseFeeApricotPhase3
		}
		// TODO: this should be removed as 256 is the maximum possible bit length of a big int
		if bfLen := ethHeader.BaseFee.BitLen(); bfLen > 256 {
			return fmt.Errorf("too large base fee: bitlen %d", bfLen)
		}
	}

	// If we are in ApricotPhase4, ensure that ExtDataGasUsed is populated correctly.
	if rules.IsApricotPhase4 {
		if rules.IsApricotPhase5 {
			if !utils.BigLessOrEqualUint64(ethHeader.ExtDataGasUsed, ap5.AtomicGasLimit) {
				return fmt.Errorf("too large extDataGasUsed: %d", ethHeader.ExtDataGasUsed)
			}
		}
		var totalGasUsed uint64
		for _, atomicTx := range b.atomicTxs {
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

		switch {
		case !utils.BigEqualUint64(ethHeader.ExtDataGasUsed, totalGasUsed):
			return fmt.Errorf("invalid extDataGasUsed: have %d, want %d", ethHeader.ExtDataGasUsed, totalGasUsed)

		// Make sure BlockGasCost is not nil
		// NOTE: ethHeader.BlockGasCost correctness is checked in header verification
		case ethHeader.BlockGasCost == nil:
			return errNilBlockGasCostApricotPhase4
		case !ethHeader.BlockGasCost.IsUint64():
			return fmt.Errorf("too large blockGasCost: %d", ethHeader.BlockGasCost)
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
		if ethHeader.BlobGasUsed == nil {
			return fmt.Errorf("blob gas used must not be nil in Cancun")
		} else if *ethHeader.BlobGasUsed > 0 {
			return fmt.Errorf("blobs not enabled on avalanche networks: used %d blob gas, expected 0", *ethHeader.BlobGasUsed)
		}
	}
	return nil
}
