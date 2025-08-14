// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/subnet-evm/consensus"
	"github.com/ava-labs/subnet-evm/consensus/misc/eip4844"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/params/extras"
	"github.com/ava-labs/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/subnet-evm/plugin/evm/vmerrors"
	"github.com/ava-labs/subnet-evm/utils"

	customheader "github.com/ava-labs/subnet-evm/plugin/evm/header"
)

var (
	allowedFutureBlockTime = 10 * time.Second // Max time from current time allowed for blocks, before they're considered future blocks

	ErrInsufficientBlockGas = errors.New("insufficient gas to cover the block cost")

	errInvalidBlockTime  = errors.New("timestamp less than parent's")
	errUnclesUnsupported = errors.New("uncles unsupported")
)

type Mode struct {
	ModeSkipHeader   bool
	ModeSkipBlockFee bool
	ModeSkipCoinbase bool
}

type (
	DummyEngine struct {
		clock         *mockable.Clock
		consensusMode Mode
	}
)

func NewDummyEngine(
	mode Mode,
	clock *mockable.Clock,
) *DummyEngine {
	return &DummyEngine{
		clock:         clock,
		consensusMode: mode,
	}
}

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

func NewFakerWithClock(clock *mockable.Clock) *DummyEngine {
	return &DummyEngine{
		clock: clock,
	}
}

func NewFakerWithMode(mode Mode) *DummyEngine {
	return &DummyEngine{
		clock:         &mockable.Clock{},
		consensusMode: mode,
	}
}

func NewFakerWithModeAndClock(mode Mode, clock *mockable.Clock) *DummyEngine {
	return &DummyEngine{
		clock:         clock,
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

// verifyCoinbase checks that the coinbase is valid for the given [header] and [parent].
func (eng *DummyEngine) verifyCoinbase(header *types.Header, parent *types.Header, chain consensus.ChainHeaderReader) error {
	if eng.consensusMode.ModeSkipCoinbase {
		return nil
	}
	// get the coinbase configured at parent
	configuredAddressAtParent, isAllowFeeRecipients, err := chain.GetCoinbaseAt(parent)
	if err != nil {
		return fmt.Errorf("failed to get coinbase at %v: %w", header.Hash(), err)
	}

	if isAllowFeeRecipients {
		// if fee recipients are allowed we don't need to check the coinbase
		return nil
	}
	// we fetch the configured coinbase at the parent's state
	// to check against the coinbase in [header].
	if configuredAddressAtParent != header.Coinbase {
		return fmt.Errorf("%w: %v does not match required coinbase address %v", vmerrors.ErrInvalidCoinbase, header.Coinbase, configuredAddressAtParent)
	}
	return nil
}

func verifyHeaderGasFields(config *extras.ChainConfig, header *types.Header, parent *types.Header, chain consensus.ChainHeaderReader) error {
	// We verify the current block by checking the parent fee config
	// this is because the current block cannot set the fee config for itself
	// Fee config might depend on the state when precompile is activated
	// but we don't know the final state while forming the block.
	// See worker package for more details.
	feeConfig, _, err := chain.GetFeeConfigAt(parent)
	if err != nil {
		return err
	}
	if err := customheader.VerifyGasUsed(config, feeConfig, parent, header); err != nil {
		return err
	}
	if err := customheader.VerifyGasLimit(config, feeConfig, parent, header); err != nil {
		return err
	}
	if err := customheader.VerifyExtraPrefix(config, parent, header); err != nil {
		return err
	}

	// Verify header.BaseFee matches the expected value.
	expectedBaseFee, err := customheader.BaseFee(config, feeConfig, parent, header.Time)
	if err != nil {
		return fmt.Errorf("failed to calculate base fee: %w", err)
	}
	if !utils.BigEqual(header.BaseFee, expectedBaseFee) {
		return fmt.Errorf("expected base fee (%d), found (%d)", expectedBaseFee, header.BaseFee)
	}

	// Enforce BlockGasCost constraints
	expectedBlockGasCost := customheader.BlockGasCost(
		config,
		feeConfig,
		parent,
		header.Time,
	)
	headerExtra := customtypes.GetHeaderExtra(header)
	if !utils.BigEqual(headerExtra.BlockGasCost, expectedBlockGasCost) {
		return fmt.Errorf("invalid block gas cost: have %d, want %d", headerExtra.BlockGasCost, expectedBlockGasCost)
	}
	return nil
}

// modified from consensus.go
func (eng *DummyEngine) verifyHeader(chain consensus.ChainHeaderReader, header *types.Header, parent *types.Header, uncle bool) error {
	// Ensure that we do not verify an uncle
	if uncle {
		return errUnclesUnsupported
	}

	// Verify the extra data is well-formed.
	config := params.GetExtra(chain.Config())
	rules := config.GetAvalancheRules(header.Time)
	if err := customheader.VerifyExtra(rules, header.Extra); err != nil {
		return err
	}

	// Ensure gas-related header fields are correct
	if err := verifyHeaderGasFields(config, header, parent, chain); err != nil {
		return err
	}
	// Ensure that coinbase is valid
	if err := eng.verifyCoinbase(header, parent, chain); err != nil {
		return err
	}

	// Verify the header's timestamp
	if header.Time > uint64(eng.clock.Time().Add(allowedFutureBlockTime).Unix()) {
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
		if *header.BlobGasUsed > 0 { // VerifyEIP4844Header ensures BlobGasUsed is non-nil
			return fmt.Errorf("blobs not enabled on avalanche networks: used %d blob gas, expected 0", *header.BlobGasUsed)
		}
	}
	return nil
}

func (*DummyEngine) Author(header *types.Header) (common.Address, error) {
	return header.Coinbase, nil
}

func (eng *DummyEngine) VerifyHeader(chain consensus.ChainHeaderReader, header *types.Header) error {
	// If we're running a full engine faking, accept any input as valid
	if eng.consensusMode.ModeSkipHeader {
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
	return eng.verifyHeader(chain, header, parent, false)
}

func (*DummyEngine) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	if len(block.Uncles()) > 0 {
		return errUnclesUnsupported
	}
	return nil
}

func (*DummyEngine) Prepare(chain consensus.ChainHeaderReader, header *types.Header) error {
	header.Difficulty = big.NewInt(1)
	return nil
}

func (eng *DummyEngine) verifyBlockFee(
	baseFee *big.Int,
	requiredBlockGasCost *big.Int,
	txs []*types.Transaction,
	receipts []*types.Receipt,
) error {
	if eng.consensusMode.ModeSkipBlockFee {
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
		// Multiply the [txFeePremium] by the gasUsed in the transaction since this gives the total coin that was paid
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

	// Require that the amount of gas purchased by the effective tips within the
	// block covers at least `requiredBlockGasCost`.
	//
	// NOTE: To determine the required block fee, multiply
	// `requiredBlockGasCost` by `baseFee`.
	if blockGas.Cmp(requiredBlockGasCost) < 0 {
		return fmt.Errorf("%w: expected %d but got %d",
			ErrInsufficientBlockGas,
			requiredBlockGasCost,
			blockGas,
		)
	}
	return nil
}

func (eng *DummyEngine) Finalize(chain consensus.ChainHeaderReader, block *types.Block, parent *types.Header, state *state.StateDB, receipts []*types.Receipt) error {
	config := params.GetExtra(chain.Config())
	timestamp := block.Time()
	// we use the parent to determine the fee config
	// since the current block has not been finalized yet.
	feeConfig, _, err := chain.GetFeeConfigAt(parent)
	if err != nil {
		return err
	}
	// Verify the BlockGasCost set in the header matches the expected value.
	blockGasCost := customtypes.BlockGasCost(block)
	expectedBlockGasCost := customheader.BlockGasCost(
		config,
		feeConfig,
		parent,
		timestamp,
	)
	if !utils.BigEqual(blockGasCost, expectedBlockGasCost) {
		return fmt.Errorf("invalid blockGasCost: have %d, want %d", blockGasCost, expectedBlockGasCost)
	}
	if config.IsSubnetEVM(timestamp) {
		// Verify the block fee was paid.
		if err := eng.verifyBlockFee(
			block.BaseFee(),
			blockGasCost,
			block.Transactions(),
			receipts,
		); err != nil {
			return err
		}
	}

	return nil
}

func (eng *DummyEngine) FinalizeAndAssemble(chain consensus.ChainHeaderReader, header *types.Header, parent *types.Header, state *state.StateDB, txs []*types.Transaction,
	uncles []*types.Header, receipts []*types.Receipt,
) (*types.Block, error) {
	// we use the parent to determine the fee config
	// since the current block has not been finalized yet.
	feeConfig, _, err := chain.GetFeeConfigAt(parent)
	if err != nil {
		return nil, err
	}
	config := params.GetExtra(chain.Config())

	// Calculate the required block gas cost for this block.
	headerExtra := customtypes.GetHeaderExtra(header)
	headerExtra.BlockGasCost = customheader.BlockGasCost(
		config,
		feeConfig,
		parent,
		header.Time,
	)
	if config.IsSubnetEVM(header.Time) {
		// Verify that this block covers the block fee.
		if err := eng.verifyBlockFee(
			header.BaseFee,
			headerExtra.BlockGasCost,
			txs,
			receipts,
		); err != nil {
			return nil, err
		}
	}

	// finalize the header.Extra
	extraPrefix, err := customheader.ExtraPrefix(config, parent, header)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate new header.Extra: %w", err)
	}
	header.Extra = append(extraPrefix, header.Extra...)

	// commit the final state root
	header.Root = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))

	// Header seems complete, assemble into a block and return
	return types.NewBlock(
		header, txs, uncles, receipts, trie.NewStackTrie(nil),
	), nil
}

func (*DummyEngine) CalcDifficulty(chain consensus.ChainHeaderReader, time uint64, parent *types.Header) *big.Int {
	return big.NewInt(1)
}

func (*DummyEngine) Close() error {
	return nil
}
