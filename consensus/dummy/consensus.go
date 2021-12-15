// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/subnet-evm/consensus"
	"github.com/ava-labs/subnet-evm/core/state"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/trie"
	"github.com/ethereum/go-ethereum/common"
)

var (
	allowedFutureBlockTime = 10 * time.Second // Max time from current time allowed for blocks, before they're considered future blocks

	errInvalidBlockTime     = errors.New("timestamp less than parent's")
	errUnclesUnsupported    = errors.New("uncles unsupported")
	errBlockGasCostNil      = errors.New("block gas cost is nil")
	errBlockGasCostTooLarge = errors.New("block gas cost is not uint64")
	errBaseFeeNil           = errors.New("base fee is nil")
)

type DummyEngine struct {
	ethFaker bool
}

func NewEngine() *DummyEngine {
	return &DummyEngine{}
}

func NewETHFaker() *DummyEngine {
	return &DummyEngine{
		ethFaker: true,
	}
}

func (self *DummyEngine) verifyHeaderGasFields(config *params.ChainConfig, header *types.Header, parent *types.Header) error {
	timestamp := new(big.Int).SetUint64(header.Time)

	// Verify that the gas limit is <= 2^63-1
	cap := uint64(0x7fffffffffffffff)
	if header.GasLimit > cap {
		return fmt.Errorf("invalid gasLimit: have %v, max %v", header.GasLimit, cap)
	}
	// Verify that the gasUsed is <= gasLimit
	if header.GasUsed > header.GasLimit {
		return fmt.Errorf("invalid gasUsed: have %d, gasLimit %d", header.GasUsed, header.GasLimit)
	}
	if config.IsSubnetEVM(timestamp) {
		expectedGasLimit := config.GetFeeConfig().GasLimit.Uint64()
		if header.GasLimit != expectedGasLimit {
			return fmt.Errorf("expected gas limit to be %d, but found %d", expectedGasLimit, header.GasLimit)
		}
	} else {
		// Verify that the gas limit remains within allowed bounds
		diff := int64(parent.GasLimit) - int64(header.GasLimit)
		if diff < 0 {
			diff *= -1
		}
		limit := parent.GasLimit / params.GasLimitBoundDivisor

		if uint64(diff) >= limit || header.GasLimit < params.MinGasLimit {
			return fmt.Errorf("invalid gas limit: have %d, want %d += %d", header.GasLimit, parent.GasLimit, limit)
		}
		// Verify BaseFee is not present before Subnet EVM
		if header.BaseFee != nil {
			return fmt.Errorf("invalid baseFee before fork: have %d, want <nil>", header.BaseFee)
		}
		if header.BlockGasCost != nil {
			return fmt.Errorf("invalid blockGasCost before fork: have %d, want <nil>", header.BlockGasCost)
		}
		return nil
	}

	// Verify baseFee and rollupWindow encoding as part of header verification
	// starting in Subnet EVM
	expectedRollupWindowBytes, expectedBaseFee, err := CalcBaseFee(config, parent, header.Time)
	if err != nil {
		return fmt.Errorf("failed to calculate base fee: %w", err)
	}
	if !bytes.Equal(expectedRollupWindowBytes, header.Extra) {
		return fmt.Errorf("expected rollup window bytes: %x, found %x", expectedRollupWindowBytes, header.Extra)
	}
	if header.BaseFee == nil {
		return errors.New("expected baseFee to be non-nil")
	}
	if bfLen := header.BaseFee.BitLen(); bfLen > 256 {
		return fmt.Errorf("too large base fee: bitlen %d", bfLen)
	}
	if header.BaseFee.Cmp(expectedBaseFee) != 0 {
		return fmt.Errorf("expected base fee (%d), found (%d)", expectedBaseFee, header.BaseFee)
	}

	// Enforce BlockGasCost constraints
	blockGasCostStep := config.GetFeeConfig().BlockGasCostStep
	targetBlockRate := config.GetFeeConfig().TargetBlockRate
	minBlockGasCost := config.GetFeeConfig().MinBlockGasCost
	maxBlockGasCost := config.GetFeeConfig().MaxBlockGasCost

	expectedBlockGasCost := calcBlockGasCost(
		targetBlockRate,
		minBlockGasCost,
		maxBlockGasCost,
		blockGasCostStep,
		parent.BlockGasCost,
		parent.Time, header.Time,
	)
	if header.BlockGasCost == nil {
		return errBlockGasCostNil
	}
	if !header.BlockGasCost.IsUint64() {
		return errBlockGasCostTooLarge
	}
	if header.BlockGasCost.Cmp(expectedBlockGasCost) != 0 {
		return fmt.Errorf("invalid block gas cost: have %d, want %d", header.BlockGasCost, expectedBlockGasCost)
	}
	return nil
}

// modified from consensus.go
func (self *DummyEngine) verifyHeader(chain consensus.ChainHeaderReader, header *types.Header, parent *types.Header, uncle bool) error {
	var (
		config    = chain.Config()
		timestamp = new(big.Int).SetUint64(header.Time)
	)
	// Ensure that we do not verify an uncle
	if uncle {
		return errUnclesUnsupported
	}
	// Ensure that the header's extra-data section is of a reasonable size
	if !config.IsSubnetEVM(timestamp) {
		if uint64(len(header.Extra)) > params.MaximumExtraDataSize {
			return fmt.Errorf("extra-data too long: %d > %d", len(header.Extra), params.MaximumExtraDataSize)
		}
	} else {
		expectedExtraDataSize := params.ExtraDataSize
		if len(header.Extra) != expectedExtraDataSize {
			return fmt.Errorf("expected extra-data field to be: %d, but found %d", expectedExtraDataSize, len(header.Extra))
		}
	}
	// Ensure gas-related header fields are correct
	if err := self.verifyHeaderGasFields(config, header, parent); err != nil {
		return err
	}
	// Verify the header's timestamp
	if header.Time > uint64(time.Now().Add(allowedFutureBlockTime).Unix()) {
		return consensus.ErrFutureBlock
	}
	// Verify the header's timestamp is not earlier than parent's
	// it does include equality(==), so multiple blocks per second is ok
	if header.Time < parent.Time {
		return errInvalidBlockTime
	}
	// Verify that the block number is parent's +1
	if diff := new(big.Int).Sub(header.Number, parent.Number); diff.Cmp(big.NewInt(1)) != 0 {
		return consensus.ErrInvalidNumber
	}
	// Verify the engine specific seal securing the block
	return self.VerifySeal(chain, header)
}

func (self *DummyEngine) Author(header *types.Header) (common.Address, error) {
	return header.Coinbase, nil
}

func (self *DummyEngine) VerifyHeader(chain consensus.ChainHeaderReader, header *types.Header) error {
	// Short circuit if the header is known, or it's parent not
	number := header.Number.Uint64()
	if chain.GetHeader(header.Hash(), number) != nil {
		return nil
	}
	parent := chain.GetHeader(header.ParentHash, number-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	// Sanity checks passed, do a proper verification
	return self.verifyHeader(chain, header, parent, false)
}

func (self *DummyEngine) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	if len(block.Uncles()) > 0 {
		return errUnclesUnsupported
	}
	return nil
}

func (self *DummyEngine) VerifySeal(chain consensus.ChainHeaderReader, header *types.Header) error {
	return nil
}

func (self *DummyEngine) Prepare(chain consensus.ChainHeaderReader, header *types.Header) error {
	header.Difficulty = big.NewInt(1)
	return nil
}

func (self *DummyEngine) verifyBlockFee(
	baseFee *big.Int,
	requiredBlockGasCost *big.Int,
	txs []*types.Transaction,
	receipts []*types.Receipt,
) error {
	if self.ethFaker {
		return nil
	}
	if baseFee == nil || baseFee.Sign() <= 0 {
		return fmt.Errorf("invalid base fee (%d) in SubnetEVM", baseFee)
	}
	if requiredBlockGasCost == nil || !requiredBlockGasCost.IsUint64() {
		return fmt.Errorf("invalid block gas cost (%d) in SubnetEVM", requiredBlockGasCost)
	}

	var (
		gasUsed              = new(big.Int)
		blockFeeContribution = new(big.Int)
		totalBlockFee        = new(big.Int)
	)
	// Calculate the total excess over the base fee that was paid towards the block fee
	for i, receipt := range receipts {
		// Each transaction contributes the excess over the baseFee towards the totalBlockFee
		// This should be equivalent to the sum of the "priority fees" within EIP-1559.
		txFeePremium, err := txs[i].EffectiveGasTip(baseFee)
		if err != nil {
			return err
		}
		blockFeeContribution.Mul(txFeePremium, gasUsed.SetUint64(receipt.GasUsed))
		totalBlockFee.Add(totalBlockFee, blockFeeContribution)
	}
	// Calculate how much gas the [totalBlockFee] would purchase at the price level
	// set by this block.
	blockGas := new(big.Int).Div(totalBlockFee, baseFee)

	// Require that the amount of gas purchased by the effective tips within the block, [blockGas],
	// covers at least [requiredBlockGasCost].
	//
	// NOTE: To determine the [requiredBlockFee], multiply [requiredBlockGasCost]
	// by [baseFee].
	if blockGas.Cmp(requiredBlockGasCost) < 0 {
		return fmt.Errorf(
			"insufficient gas (%d) to cover the block cost (%d) at base fee (%d) (total block fee: %d)",
			blockGas, requiredBlockGasCost, baseFee, totalBlockFee,
		)
	}
	return nil
}

func (self *DummyEngine) Finalize(chain consensus.ChainHeaderReader, block *types.Block, parent *types.Header, state *state.StateDB, receipts []*types.Receipt) error {
	if chain.Config().IsSubnetEVM(new(big.Int).SetUint64(block.Time())) {
		blockGasCostStep := chain.Config().GetFeeConfig().BlockGasCostStep
		targetBlockRate := chain.Config().GetFeeConfig().TargetBlockRate
		minBlockGasCost := chain.Config().GetFeeConfig().MinBlockGasCost
		maxBlockGasCost := chain.Config().GetFeeConfig().MaxBlockGasCost
		blockGasCost := calcBlockGasCost(
			targetBlockRate,
			minBlockGasCost,
			maxBlockGasCost,
			blockGasCostStep,
			parent.BlockGasCost,
			parent.Time, block.Time(),
		)
		if blockBlockGasCost := block.BlockGasCost(); blockBlockGasCost == nil || !blockBlockGasCost.IsUint64() || blockBlockGasCost.Cmp(blockGasCost) != 0 {
			return fmt.Errorf("invalid blockGasCost: have %d, want %d", blockBlockGasCost, blockGasCost)
		}
		if err := self.verifyBlockFee(
			block.BaseFee(),
			block.BlockGasCost(),
			block.Transactions(),
			receipts,
		); err != nil {
			return err
		}
	}

	return nil
}

func (self *DummyEngine) FinalizeAndAssemble(chain consensus.ChainHeaderReader, header *types.Header, parent *types.Header, state *state.StateDB, txs []*types.Transaction,
	uncles []*types.Header, receipts []*types.Receipt) (*types.Block, error) {
	if chain.Config().IsSubnetEVM(new(big.Int).SetUint64(header.Time)) {
		blockGasCostStep := chain.Config().GetFeeConfig().BlockGasCostStep
		targetBlockRate := chain.Config().GetFeeConfig().TargetBlockRate
		minBlockGasCost := chain.Config().GetFeeConfig().MinBlockGasCost
		maxBlockGasCost := chain.Config().GetFeeConfig().MaxBlockGasCost
		header.BlockGasCost = calcBlockGasCost(
			targetBlockRate,
			minBlockGasCost,
			maxBlockGasCost,
			blockGasCostStep,
			parent.BlockGasCost,
			parent.Time, header.Time,
		)
		if err := self.verifyBlockFee(
			header.BaseFee,
			header.BlockGasCost,
			txs,
			receipts,
		); err != nil {
			return nil, err
		}
	}
	// commit the final state root
	header.Root = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))

	// Header seems complete, assemble into a block and return
	return types.NewBlock(
		header, txs, uncles, receipts, new(trie.Trie),
	), nil
}

func (self *DummyEngine) CalcDifficulty(chain consensus.ChainHeaderReader, time uint64, parent *types.Header) *big.Int {
	return big.NewInt(1)
}

func (self *DummyEngine) Close() error {
	return nil
}
