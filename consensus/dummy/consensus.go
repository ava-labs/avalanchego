// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/consensus/misc/eip4844"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/trie"

	"github.com/ava-labs/coreth/consensus"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/plugin/evm/customheader"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/utils"
)

var (
	allowedFutureBlockTime = 10 * time.Second // Max time from current time allowed for blocks, before they're considered future blocks

	errInvalidBlockTime       = errors.New("timestamp less than parent's")
	errUnclesUnsupported      = errors.New("uncles unsupported")
	errExtDataGasUsedNil      = errors.New("extDataGasUsed is nil")
	errExtDataGasUsedTooLarge = errors.New("extDataGasUsed is not uint64")
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
		clock               *mockable.Clock
		consensusMode       Mode
		desiredTargetExcess *gas.Gas
	}
)

func NewDummyEngine(
	cb ConsensusCallbacks,
	mode Mode,
	clock *mockable.Clock,
	desiredTargetExcess *gas.Gas,
) *DummyEngine {
	return &DummyEngine{
		cb:                  cb,
		clock:               clock,
		consensusMode:       mode,
		desiredTargetExcess: desiredTargetExcess,
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

func verifyHeaderGasFields(config *extras.ChainConfig, header *types.Header, parent *types.Header) error {
	if err := customheader.VerifyGasUsed(config, parent, header); err != nil {
		return err
	}
	if err := customheader.VerifyGasLimit(config, parent, header); err != nil {
		return err
	}
	if err := customheader.VerifyExtraPrefix(config, parent, header); err != nil {
		return err
	}

	// Verify header.BaseFee matches the expected value.
	expectedBaseFee, err := customheader.BaseFee(config, parent, header.Time)
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
func (eng *DummyEngine) verifyHeader(chain consensus.ChainHeaderReader, header *types.Header, parent *types.Header, uncle bool) error {
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
	cancun := config.IsCancun(header.Number, header.Time)
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
		return fmt.Errorf("invalid blockGasCost: have %d, want %d", blockGasCost, expectedBlockGasCost)
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
			return fmt.Errorf("invalid extDataGasUsed: have %d, want %d", blockExtDataGasUsed, extDataGasUsed)
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
