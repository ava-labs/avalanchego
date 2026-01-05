// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/trie"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/consensus"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customheader"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/vmerrors"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
)

var (
	errUnclesUnsupported   = errors.New("uncles unsupported")
	ErrInvalidBlockGasCost = errors.New("invalid blockGasCost")
)

type Mode struct {
	ModeSkipHeader   bool
	ModeSkipBlockFee bool
	ModeSkipCoinbase bool
}

type (
	OnFinalizeAndAssembleCallbackType = func(
		header *types.Header,
		parent *types.Header,
		state *state.StateDB,
		txs []*types.Transaction,
	) (
		extraData []byte,
		blockFeeContribution *big.Int,
		extDataGasUsed *big.Int,
		err error,
	)

	OnExtraStateChangeType = func(
		block *types.Block,
		parent *types.Header,
		statedb *state.StateDB,
	) (
		blockFeeContribution *big.Int,
		extDataGasUsed *big.Int,
		err error,
	)

	ConsensusCallbacks struct {
		OnFinalizeAndAssemble OnFinalizeAndAssembleCallbackType
		OnExtraStateChange    OnExtraStateChangeType
	}

	DummyEngine struct {
		consensusMode      Mode
		desiredDelayExcess *acp226.DelayExcess
	}
)

func NewDummyEngine(
	mode Mode,
	desiredDelayExcess *acp226.DelayExcess, // Guides the min delay excess (ACP-226) toward the desired value
) *DummyEngine {
	return &DummyEngine{
		consensusMode:      mode,
		desiredDelayExcess: desiredDelayExcess,
	}
}

func NewETHFaker() *DummyEngine {
	return &DummyEngine{
		consensusMode: Mode{ModeSkipBlockFee: true},
	}
}

func NewFaker() *DummyEngine {
	return &DummyEngine{}
}

func NewFakerWithMode(mode Mode) *DummyEngine {
	return &DummyEngine{
		consensusMode: mode,
	}
}

func NewCoinbaseFaker() *DummyEngine {
	return &DummyEngine{
		consensusMode: Mode{ModeSkipCoinbase: true},
	}
}

func NewFullFaker() *DummyEngine {
	return &DummyEngine{
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
	// Verifying the gas used occurs earlier in the block validation process in verifyIntrinsicGas, so
	// customheader.VerifyGasUsed is not called here.
	if err := customheader.VerifyGasLimit(config, feeConfig, parent, header); err != nil {
		return err
	}
	if err := customheader.VerifyExtraPrefix(config, parent, header); err != nil {
		return err
	}

	// Verify header.BaseFee matches the expected value.
	timeMS := customtypes.HeaderTimeMilliseconds(header)
	expectedBaseFee, err := customheader.BaseFee(config, feeConfig, parent, timeMS)
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

	// Verify that the block number is parent's +1
	if diff := new(big.Int).Sub(header.Number, parent.Number); diff.Cmp(big.NewInt(1)) != 0 {
		return consensus.ErrInvalidNumber
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

func (*DummyEngine) VerifyUncles(_ consensus.ChainReader, block *types.Block) error {
	if len(block.Uncles()) > 0 {
		return errUnclesUnsupported
	}
	return nil
}

func (*DummyEngine) Prepare(_ consensus.ChainHeaderReader, header *types.Header) error {
	header.Difficulty = big.NewInt(1)
	return nil
}

func (eng *DummyEngine) Finalize(chain consensus.ChainHeaderReader, block *types.Block, parent *types.Header, _ *state.StateDB, receipts []*types.Receipt) error {
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
		return fmt.Errorf("%w: have %d, want %d", ErrInvalidBlockGasCost, blockGasCost, expectedBlockGasCost)
	}
	if config.IsSubnetEVM(timestamp) {
		// Verify the block fee was paid.
		if !eng.consensusMode.ModeSkipBlockFee {
			if err := customheader.VerifyBlockFee(
				block.BaseFee(),
				blockGasCost,
				block.Transactions(),
				receipts,
				nil,
			); err != nil {
				return err
			}
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

	configExtra := params.GetExtra(chain.Config())
	headerExtra := customtypes.GetHeaderExtra(header)
	// Calculate the required block gas cost for this block.
	headerExtra.BlockGasCost = customheader.BlockGasCost(
		configExtra,
		feeConfig,
		parent,
		header.Time,
	)
	if configExtra.IsSubnetEVM(header.Time) {
		// Verify that this block covers the block fee.
		if !eng.consensusMode.ModeSkipBlockFee {
			if err := customheader.VerifyBlockFee(
				header.BaseFee,
				headerExtra.BlockGasCost,
				txs,
				receipts,
				nil,
			); err != nil {
				return nil, err
			}
		}
	}

	// finalize the header.Extra
	extraPrefix, err := customheader.ExtraPrefix(configExtra, parent, header)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate new header.Extra: %w", err)
	}
	header.Extra = append(extraPrefix, header.Extra...)

	// Set the min delay excess
	minDelayExcess, err := customheader.MinDelayExcess(
		configExtra,
		parent,
		header.Time,
		eng.desiredDelayExcess,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate min delay excess: %w", err)
	}
	headerExtra.MinDelayExcess = minDelayExcess

	// commit the final state root
	header.Root = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))

	// Header seems complete, assemble into a block and return
	return types.NewBlock(
		header, txs, uncles, receipts, trie.NewStackTrie(nil),
	), nil
}

//nolint:revive // General-purpose types lose the meaning of args if unused ones are removed
func (*DummyEngine) CalcDifficulty(chain consensus.ChainHeaderReader, time uint64, parent *types.Header) *big.Int {
	return big.NewInt(1)
}

func (*DummyEngine) Close() error {
	return nil
}
