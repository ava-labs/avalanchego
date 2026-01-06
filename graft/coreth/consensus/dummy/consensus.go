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

	"github.com/ava-labs/avalanchego/graft/coreth/consensus"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customheader"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/coreth/utils"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
)

var (
	errUnclesUnsupported      = errors.New("uncles unsupported")
	errExtDataGasUsedNil      = errors.New("extDataGasUsed is nil")
	errExtDataGasUsedTooLarge = errors.New("extDataGasUsed is not uint64")
	ErrInvalidBlockGasCost    = errors.New("invalid blockGasCost")
	errInvalidExtDataGasUsed  = errors.New("invalid extDataGasUsed")
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
		cb                  ConsensusCallbacks
		consensusMode       Mode
		desiredTargetExcess *gas.Gas
		desiredDelayExcess  *acp226.DelayExcess
	}
)

func NewDummyEngine(
	cb ConsensusCallbacks,
	mode Mode,
	desiredTargetExcess *gas.Gas, // Guides the target gas excess (ACP-176) toward the desired value
	desiredDelayExcess *acp226.DelayExcess, // Guides the min delay excess (ACP-226) toward the desired value
) *DummyEngine {
	return &DummyEngine{
		cb:                  cb,
		consensusMode:       mode,
		desiredTargetExcess: desiredTargetExcess,
		desiredDelayExcess:  desiredDelayExcess,
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

func NewFakerWithCallbacks(cb ConsensusCallbacks) *DummyEngine {
	return &DummyEngine{
		cb: cb,
	}
}

func NewFakerWithMode(cb ConsensusCallbacks, mode Mode) *DummyEngine {
	return &DummyEngine{
		cb:            cb,
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

func verifyHeaderGasFields(config *extras.ChainConfig, header *types.Header, parent *types.Header) error {
	// Verifying the gas used occurs earlier in the block validation process in verifyIntrinsicGas, so
	// customheader.VerifyGasUsed is not called here.
	if err := customheader.VerifyGasLimit(config, parent, header); err != nil {
		return err
	}
	if err := customheader.VerifyExtraPrefix(config, parent, header); err != nil {
		return err
	}

	// Verify header.BaseFee matches the expected value.
	timeMS := customtypes.HeaderTimeMilliseconds(header)
	expectedBaseFee, err := customheader.BaseFee(config, parent, timeMS)
	if err != nil {
		return fmt.Errorf("failed to calculate base fee: %w", err)
	}
	if !utils.BigEqual(header.BaseFee, expectedBaseFee) {
		return fmt.Errorf("expected base fee %d, found %d", expectedBaseFee, header.BaseFee)
	}

	headerExtra := customtypes.GetHeaderExtra(header)

	// Enforce BlockGasCost constraints
	expectedBlockGasCost := customheader.BlockGasCost(
		config,
		parent,
		header.Time,
	)
	if !utils.BigEqual(headerExtra.BlockGasCost, expectedBlockGasCost) {
		return fmt.Errorf("invalid block gas cost: have %d, want %d", headerExtra.BlockGasCost, expectedBlockGasCost)
	}

	// Verify ExtDataGasUsed not present before AP4
	if !config.IsApricotPhase4(header.Time) {
		if headerExtra.ExtDataGasUsed != nil {
			return fmt.Errorf("invalid extDataGasUsed before fork: have %d, want <nil>", headerExtra.ExtDataGasUsed)
		}
		return nil
	}

	// ExtDataGasUsed correctness is checked during block validation
	// (when the validator has access to the block contents)
	if headerExtra.ExtDataGasUsed == nil {
		return errExtDataGasUsedNil
	}
	if !headerExtra.ExtDataGasUsed.IsUint64() {
		return errExtDataGasUsedTooLarge
	}
	return nil
}

// modified from consensus.go
func verifyHeader(chain consensus.ChainHeaderReader, header *types.Header, parent *types.Header, uncle bool) error {
	// Ensure that we do not verify an uncle
	if uncle {
		return errUnclesUnsupported
	}

	// Verify the extra data is well-formed.
	config := chain.Config()
	configExtra := params.GetExtra(config)
	rules := configExtra.GetAvalancheRules(header.Time)
	if err := customheader.VerifyExtra(rules, header.Extra); err != nil {
		return err
	}

	// Ensure gas-related header fields are correct
	if err := verifyHeaderGasFields(configExtra, header, parent); err != nil {
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
	return verifyHeader(chain, header, parent, false)
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

func (eng *DummyEngine) Finalize(chain consensus.ChainHeaderReader, block *types.Block, parent *types.Header, state *state.StateDB, receipts []*types.Receipt) error {
	// Perform extra state change while finalizing the block
	var (
		contribution, extDataGasUsed *big.Int
		err                          error
	)
	if eng.cb.OnExtraStateChange != nil {
		contribution, extDataGasUsed, err = eng.cb.OnExtraStateChange(block, parent, state)
		if err != nil {
			return err
		}
	}

	config := params.GetExtra(chain.Config())
	timestamp := block.Time()
	// Verify the BlockGasCost set in the header matches the expected value.
	blockGasCost := customtypes.BlockGasCost(block)
	expectedBlockGasCost := customheader.BlockGasCost(
		config,
		parent,
		timestamp,
	)
	if !utils.BigEqual(blockGasCost, expectedBlockGasCost) {
		return fmt.Errorf("%w: have %d, want %d", ErrInvalidBlockGasCost, blockGasCost, expectedBlockGasCost)
	}
	if config.IsApricotPhase4(timestamp) {
		// Validate extDataGasUsed and BlockGasCost match expectations
		//
		// NOTE: This is a duplicate check of what is already performed in
		// blockValidator but is done here for symmetry with FinalizeAndAssemble.
		if extDataGasUsed == nil {
			extDataGasUsed = new(big.Int).Set(common.Big0)
		}
		if blockExtDataGasUsed := customtypes.BlockExtDataGasUsed(block); blockExtDataGasUsed == nil || !blockExtDataGasUsed.IsUint64() || blockExtDataGasUsed.Cmp(extDataGasUsed) != 0 {
			return fmt.Errorf("%w: have %d, want %d", errInvalidExtDataGasUsed, blockExtDataGasUsed, extDataGasUsed)
		}

		// Verify the block fee was paid.
		if !eng.consensusMode.ModeSkipBlockFee {
			if err := customheader.VerifyBlockFee(
				block.BaseFee(),
				blockGasCost,
				block.Transactions(),
				receipts,
				contribution,
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
	var (
		contribution, extDataGasUsed *big.Int
		extraData                    []byte
		err                          error
	)
	if eng.cb.OnFinalizeAndAssemble != nil {
		extraData, contribution, extDataGasUsed, err = eng.cb.OnFinalizeAndAssemble(header, parent, state, txs)
		if err != nil {
			return nil, err
		}
	}

	configExtra := params.GetExtra(chain.Config())
	headerExtra := customtypes.GetHeaderExtra(header)
	// Calculate the required block gas cost for this block.
	headerExtra.BlockGasCost = customheader.BlockGasCost(
		configExtra,
		parent,
		header.Time,
	)
	if configExtra.IsApricotPhase4(header.Time) {
		headerExtra.ExtDataGasUsed = extDataGasUsed
		if headerExtra.ExtDataGasUsed == nil {
			headerExtra.ExtDataGasUsed = new(big.Int)
		}

		// Verify that this block covers the block fee.
		if !eng.consensusMode.ModeSkipBlockFee {
			if err := customheader.VerifyBlockFee(
				header.BaseFee,
				headerExtra.BlockGasCost,
				txs,
				receipts,
				contribution,
			); err != nil {
				return nil, err
			}
		}
	}

	// finalize the header.Extra
	extraPrefix, err := customheader.ExtraPrefix(configExtra, parent, header, eng.desiredTargetExcess)
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
	return customtypes.NewBlockWithExtData(
		header, txs, uncles, receipts, trie.NewStackTrie(nil),
		extraData, configExtra.IsApricotPhase1(header.Time),
	), nil
}

func (*DummyEngine) CalcDifficulty(_ consensus.ChainHeaderReader, _ uint64, _ *types.Header) *big.Int {
	return big.NewInt(1)
}

func (*DummyEngine) Close() error {
	return nil
}
