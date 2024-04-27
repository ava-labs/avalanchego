// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/coreth/consensus"
	"github.com/ava-labs/coreth/consensus/misc/eip4844"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
)

var (
	allowedFutureBlockTime = 10 * time.Second // Max time from current time allowed for blocks, before they're considered future blocks

	errInvalidBlockTime       = errors.New("timestamp less than parent's")
	errUnclesUnsupported      = errors.New("uncles unsupported")
	errBlockGasCostNil        = errors.New("block gas cost is nil")
	errBlockGasCostTooLarge   = errors.New("block gas cost is not uint64")
	errBaseFeeNil             = errors.New("base fee is nil")
	errExtDataGasUsedNil      = errors.New("extDataGasUsed is nil")
	errExtDataGasUsedTooLarge = errors.New("extDataGasUsed is not uint64")
)

type Mode struct {
	ModeSkipHeader   bool
	ModeSkipBlockFee bool
	ModeSkipCoinbase bool
}

type (
	OnFinalizeAndAssembleCallbackType = func(header *types.Header, state *state.StateDB, txs []*types.Transaction) (extraData []byte, blockFeeContribution *big.Int, extDataGasUsed *big.Int, err error)
	OnExtraStateChangeType            = func(block *types.Block, statedb *state.StateDB) (blockFeeContribution *big.Int, extDataGasUsed *big.Int, err error)

	ConsensusCallbacks struct {
		OnFinalizeAndAssemble OnFinalizeAndAssembleCallbackType
		OnExtraStateChange    OnExtraStateChangeType
	}

	DummyEngine struct {
		cb            ConsensusCallbacks
		clock         *mockable.Clock
		consensusMode Mode
	}
)

func NewETHFaker() *DummyEngine {
	return &DummyEngine{
		clock:         &mockable.Clock{},
		consensusMode: Mode{ModeSkipBlockFee: true},
	}
}

func NewFaker() *DummyEngine {
	return &DummyEngine{
		clock: &mockable.Clock{},
	}
}

func NewFakerWithClock(cb ConsensusCallbacks, clock *mockable.Clock) *DummyEngine {
	return &DummyEngine{
		cb:    cb,
		clock: clock,
	}
}

func NewFakerWithCallbacks(cb ConsensusCallbacks) *DummyEngine {
	return &DummyEngine{
		cb:    cb,
		clock: &mockable.Clock{},
	}
}

func NewFakerWithMode(cb ConsensusCallbacks, mode Mode) *DummyEngine {
	return &DummyEngine{
		cb:            cb,
		clock:         &mockable.Clock{},
		consensusMode: mode,
	}
}

func NewCoinbaseFaker() *DummyEngine {
	return &DummyEngine{
		clock:         &mockable.Clock{},
		consensusMode: Mode{ModeSkipCoinbase: true},
	}
}

func NewFullFaker() *DummyEngine {
	return &DummyEngine{
		clock:         &mockable.Clock{},
		consensusMode: Mode{ModeSkipHeader: true},
	}
}

func (self *DummyEngine) verifyHeaderGasFields(config *params.ChainConfig, header *types.Header, parent *types.Header) error {
	// Verify that the gas limit is <= 2^63-1
	if header.GasLimit > params.MaxGasLimit {
		return fmt.Errorf("invalid gasLimit: have %v, max %v", header.GasLimit, params.MaxGasLimit)
	}
	// Verify that the gasUsed is <= gasLimit
	if header.GasUsed > header.GasLimit {
		return fmt.Errorf("invalid gasUsed: have %d, gasLimit %d", header.GasUsed, header.GasLimit)
	}
	if config.IsCortina(header.Time) {
		if header.GasLimit != params.CortinaGasLimit {
			return fmt.Errorf("expected gas limit to be %d in Cortina, but found %d", params.CortinaGasLimit, header.GasLimit)
		}
	} else if config.IsApricotPhase1(header.Time) {
		if header.GasLimit != params.ApricotPhase1GasLimit {
			return fmt.Errorf("expected gas limit to be %d in ApricotPhase1, but found %d", params.ApricotPhase1GasLimit, header.GasLimit)
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
	}

	if !config.IsApricotPhase3(header.Time) {
		// Verify BaseFee is not present before AP3
		if header.BaseFee != nil {
			return fmt.Errorf("invalid baseFee before fork: have %d, want <nil>", header.BaseFee)
		}
	} else {
		// Verify baseFee and rollupWindow encoding as part of header verification
		// starting in AP3
		expectedRollupWindowBytes, expectedBaseFee, err := CalcBaseFee(config, parent, header.Time)
		if err != nil {
			return fmt.Errorf("failed to calculate base fee: %w", err)
		}
		if len(header.Extra) < len(expectedRollupWindowBytes) || !bytes.Equal(expectedRollupWindowBytes, header.Extra[:len(expectedRollupWindowBytes)]) {
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
	}

	// Verify BlockGasCost, ExtDataGasUsed not present before AP4
	if !config.IsApricotPhase4(header.Time) {
		if header.BlockGasCost != nil {
			return fmt.Errorf("invalid blockGasCost before fork: have %d, want <nil>", header.BlockGasCost)
		}
		if header.ExtDataGasUsed != nil {
			return fmt.Errorf("invalid extDataGasUsed before fork: have %d, want <nil>", header.ExtDataGasUsed)
		}
		return nil
	}

	// Enforce BlockGasCost constraints
	blockGasCostStep := ApricotPhase4BlockGasCostStep
	if config.IsApricotPhase5(header.Time) {
		blockGasCostStep = ApricotPhase5BlockGasCostStep
	}
	expectedBlockGasCost := calcBlockGasCost(
		ApricotPhase4TargetBlockRate,
		ApricotPhase4MinBlockGasCost,
		ApricotPhase4MaxBlockGasCost,
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
	// ExtDataGasUsed correctness is checked during block validation
	// (when the validator has access to the block contents)
	if header.ExtDataGasUsed == nil {
		return errExtDataGasUsedNil
	}
	if !header.ExtDataGasUsed.IsUint64() {
		return errExtDataGasUsedTooLarge
	}
	return nil
}

// modified from consensus.go
func (self *DummyEngine) verifyHeader(chain consensus.ChainHeaderReader, header *types.Header, parent *types.Header, uncle bool) error {
	config := chain.Config()
	// Ensure that we do not verify an uncle
	if uncle {
		return errUnclesUnsupported
	}
	switch {
	case config.IsDurango(header.Time):
		if len(header.Extra) < params.DynamicFeeExtraDataSize {
			return fmt.Errorf("expected extra-data field length >= %d, found %d", params.DynamicFeeExtraDataSize, len(header.Extra))
		}
	case config.IsApricotPhase3(header.Time):
		if len(header.Extra) != params.DynamicFeeExtraDataSize {
			return fmt.Errorf("expected extra-data field to be: %d, but found %d", params.DynamicFeeExtraDataSize, len(header.Extra))
		}
	default:
		if uint64(len(header.Extra)) > params.MaximumExtraDataSize {
			return fmt.Errorf("extra-data too long: %d > %d", len(header.Extra), params.MaximumExtraDataSize)
		}
	}
	// Ensure gas-related header fields are correct
	if err := self.verifyHeaderGasFields(config, header, parent); err != nil {
		return err
	}

	// Verify the header's timestamp
	if header.Time > uint64(self.clock.Time().Add(allowedFutureBlockTime).Unix()) {
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
	// Verify the existence / non-existence of excessBlobGas
	cancun := chain.Config().IsCancun(header.Number, header.Time)
	if !cancun {
		switch {
		case header.ExcessBlobGas != nil:
			return fmt.Errorf("invalid excessBlobGas: have %d, expected nil", *header.ExcessBlobGas)
		case header.BlobGasUsed != nil:
			return fmt.Errorf("invalid blobGasUsed: have %d, expected nil", *header.BlobGasUsed)
		case header.ParentBeaconRoot != nil:
			return fmt.Errorf("invalid parentBeaconRoot, have %#x, expected nil", *header.ParentBeaconRoot)
		}
	} else {
		if header.ParentBeaconRoot == nil {
			return errors.New("header is missing beaconRoot")
		}
		if *header.ParentBeaconRoot != (common.Hash{}) {
			return fmt.Errorf("invalid parentBeaconRoot, have %#x, expected empty", *header.ParentBeaconRoot)
		}
		if err := eip4844.VerifyEIP4844Header(parent, header); err != nil {
			return err
		}
	}
	return nil
}

func (self *DummyEngine) Author(header *types.Header) (common.Address, error) {
	return header.Coinbase, nil
}

func (self *DummyEngine) VerifyHeader(chain consensus.ChainHeaderReader, header *types.Header) error {
	// If we're running a full engine faking, accept any input as valid
	if self.consensusMode.ModeSkipHeader {
		return nil
	}
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

func (self *DummyEngine) Prepare(chain consensus.ChainHeaderReader, header *types.Header) error {
	header.Difficulty = big.NewInt(1)
	return nil
}

func (self *DummyEngine) verifyBlockFee(
	baseFee *big.Int,
	requiredBlockGasCost *big.Int,
	txs []*types.Transaction,
	receipts []*types.Receipt,
	extraStateChangeContribution *big.Int,
) error {
	if self.consensusMode.ModeSkipBlockFee {
		return nil
	}
	if baseFee == nil || baseFee.Sign() <= 0 {
		return fmt.Errorf("invalid base fee (%d) in apricot phase 4", baseFee)
	}
	if requiredBlockGasCost == nil || !requiredBlockGasCost.IsUint64() {
		return fmt.Errorf("invalid block gas cost (%d) in apricot phase 4", requiredBlockGasCost)
	}

	var (
		gasUsed              = new(big.Int)
		blockFeeContribution = new(big.Int)
		totalBlockFee        = new(big.Int)
	)

	// Add in the external contribution
	if extraStateChangeContribution != nil {
		if extraStateChangeContribution.Cmp(common.Big0) < 0 {
			return fmt.Errorf("invalid extra state change contribution: %d", extraStateChangeContribution)
		}
		totalBlockFee.Add(totalBlockFee, extraStateChangeContribution)
	}

	// Calculate the total excess (denominated in AVAX) over the base fee that was paid towards the block fee
	for i, receipt := range receipts {
		// Each transaction contributes the excess over the baseFee towards the totalBlockFee
		// This should be equivalent to the sum of the "priority fees" within EIP-1559.
		txFeePremium, err := txs[i].EffectiveGasTip(baseFee)
		if err != nil {
			return err
		}
		// Multiply the [txFeePremium] by the gasUsed in the transaction since this gives the total AVAX that was paid
		// above the amount required if the transaction had simply paid the minimum base fee for the block.
		//
		// Ex. LegacyTx paying a gas price of 100 gwei for 1M gas in a block with a base fee of 10 gwei.
		// Total Fee = 100 gwei * 1M gas
		// Minimum Fee = 10 gwei * 1M gas (minimum fee that would have been accepted for this transaction)
		// Fee Premium = 90 gwei
		// Total Overpaid = 90 gwei * 1M gas

		blockFeeContribution.Mul(txFeePremium, gasUsed.SetUint64(receipt.GasUsed))
		totalBlockFee.Add(totalBlockFee, blockFeeContribution)
	}
	// Calculate how much gas the [totalBlockFee] would purchase at the price level
	// set by the base fee of this block.
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
	// Perform extra state change while finalizing the block
	var (
		contribution, extDataGasUsed *big.Int
		err                          error
	)
	if self.cb.OnExtraStateChange != nil {
		contribution, extDataGasUsed, err = self.cb.OnExtraStateChange(block, state)
		if err != nil {
			return err
		}
	}
	if chain.Config().IsApricotPhase4(block.Time()) {
		// Validate extDataGasUsed and BlockGasCost match expectations
		//
		// NOTE: This is a duplicate check of what is already performed in
		// blockValidator but is done here for symmetry with FinalizeAndAssemble.
		if extDataGasUsed == nil {
			extDataGasUsed = new(big.Int).Set(common.Big0)
		}
		if blockExtDataGasUsed := block.ExtDataGasUsed(); blockExtDataGasUsed == nil || !blockExtDataGasUsed.IsUint64() || blockExtDataGasUsed.Cmp(extDataGasUsed) != 0 {
			return fmt.Errorf("invalid extDataGasUsed: have %d, want %d", blockExtDataGasUsed, extDataGasUsed)
		}
		blockGasCostStep := ApricotPhase4BlockGasCostStep
		if chain.Config().IsApricotPhase5(block.Time()) {
			blockGasCostStep = ApricotPhase5BlockGasCostStep
		}
		// Calculate the expected blockGasCost for this block.
		// Note: this is a deterministic transtion that defines an exact block fee for this block.
		blockGasCost := calcBlockGasCost(
			ApricotPhase4TargetBlockRate,
			ApricotPhase4MinBlockGasCost,
			ApricotPhase4MaxBlockGasCost,
			blockGasCostStep,
			parent.BlockGasCost,
			parent.Time, block.Time(),
		)
		// Verify the BlockGasCost set in the header matches the calculated value.
		if blockBlockGasCost := block.BlockGasCost(); blockBlockGasCost == nil || !blockBlockGasCost.IsUint64() || blockBlockGasCost.Cmp(blockGasCost) != 0 {
			return fmt.Errorf("invalid blockGasCost: have %d, want %d", blockBlockGasCost, blockGasCost)
		}
		// Verify the block fee was paid.
		if err := self.verifyBlockFee(
			block.BaseFee(),
			block.BlockGasCost(),
			block.Transactions(),
			receipts,
			contribution,
		); err != nil {
			return err
		}
	}

	return nil
}

func (self *DummyEngine) FinalizeAndAssemble(chain consensus.ChainHeaderReader, header *types.Header, parent *types.Header, state *state.StateDB, txs []*types.Transaction,
	uncles []*types.Header, receipts []*types.Receipt,
) (*types.Block, error) {
	var (
		contribution, extDataGasUsed *big.Int
		extraData                    []byte
		err                          error
	)
	if self.cb.OnFinalizeAndAssemble != nil {
		extraData, contribution, extDataGasUsed, err = self.cb.OnFinalizeAndAssemble(header, state, txs)
		if err != nil {
			return nil, err
		}
	}
	if chain.Config().IsApricotPhase4(header.Time) {
		header.ExtDataGasUsed = extDataGasUsed
		if header.ExtDataGasUsed == nil {
			header.ExtDataGasUsed = new(big.Int).Set(common.Big0)
		}
		blockGasCostStep := ApricotPhase4BlockGasCostStep
		if chain.Config().IsApricotPhase5(header.Time) {
			blockGasCostStep = ApricotPhase5BlockGasCostStep
		}
		// Calculate the required block gas cost for this block.
		header.BlockGasCost = calcBlockGasCost(
			ApricotPhase4TargetBlockRate,
			ApricotPhase4MinBlockGasCost,
			ApricotPhase4MaxBlockGasCost,
			blockGasCostStep,
			parent.BlockGasCost,
			parent.Time, header.Time,
		)
		// Verify that this block covers the block fee.
		if err := self.verifyBlockFee(
			header.BaseFee,
			header.BlockGasCost,
			txs,
			receipts,
			contribution,
		); err != nil {
			return nil, err
		}
	}
	// commit the final state root
	header.Root = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))

	// Header seems complete, assemble into a block and return
	return types.NewBlockWithExtData(
		header, txs, uncles, receipts, trie.NewStackTrie(nil),
		extraData, chain.Config().IsApricotPhase1(header.Time),
	), nil
}

func (self *DummyEngine) CalcDifficulty(chain consensus.ChainHeaderReader, time uint64, parent *types.Header) *big.Int {
	return big.NewInt(1)
}

func (self *DummyEngine) Close() error {
	return nil
}
