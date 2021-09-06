// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/coreth/consensus"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/rpc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/trie"
)

var (
	allowedFutureBlockTime = 10 * time.Second // Max time from current time allowed for blocks, before they're considered future blocks

	errInvalidBlockTime  = errors.New("timestamp less than parent's")
	errUnclesUnsupported = errors.New("uncles unsupported")
)

type (
	OnFinalizeAndAssembleCallbackType = func(header *types.Header, state *state.StateDB, txs []*types.Transaction) (extraData []byte, blockFeeContribution *big.Int, err error)
	OnAPIsCallbackType                = func(consensus.ChainHeaderReader) []rpc.API
	OnExtraStateChangeType            = func(block *types.Block, statedb *state.StateDB) (blockFeeContribution *big.Int, err error)
	ExtraStateGasUsedType             = func(block *types.Block) (gasUsed uint64, err error)

	ConsensusCallbacks struct {
		OnAPIs                OnAPIsCallbackType
		OnFinalizeAndAssemble OnFinalizeAndAssembleCallbackType
		OnExtraStateChange    OnExtraStateChangeType
		ExtraStateGasUsed     ExtraStateGasUsedType
	}

	DummyEngine struct {
		cb           *ConsensusCallbacks
		skipBlockFee bool
	}
)

func NewDummyEngine(cb *ConsensusCallbacks) *DummyEngine {
	return &DummyEngine{
		cb: cb,
	}
}

func NewFakerSkipBlockFee() *DummyEngine {
	return &DummyEngine{
		cb:           new(ConsensusCallbacks),
		skipBlockFee: true,
	}
}

func NewFaker() *DummyEngine {
	return NewDummyEngine(new(ConsensusCallbacks))
}

// modified from consensus.go
func (self *DummyEngine) verifyHeader(chain consensus.ChainHeaderReader, header *types.Header, parent *types.Block, uncle bool) error {
	// Ensure that we do not verify an uncle
	if uncle {
		return errUnclesUnsupported
	}
	// Ensure that the header's extra-data section is of a reasonable size
	if !chain.Config().IsApricotPhase3(new(big.Int).SetUint64(header.Time)) {
		if uint64(len(header.Extra)) > params.MaximumExtraDataSize {
			return fmt.Errorf("extra-data too long: %d > %d", len(header.Extra), params.MaximumExtraDataSize)
		}
		// Verify BaseFee is not present before EIP-1559
		// Note: this has been moved up from below in order to only switch on IsApricotPhase3 once.
		if header.BaseFee != nil {
			return fmt.Errorf("invalid baseFee before fork: have %d, want <nil>", header.BaseFee)
		}
		if header.BlockGasCost != nil {
			return fmt.Errorf("invalid blockGasCost before fork: have %d, want <nil>", header.BlockGasCost)
		}
		if header.ExtDataGasUsed != nil {
			return fmt.Errorf("invalid extDataGasUsed before fork: have %d, want <nil>", header.ExtDataGasUsed)
		}
	} else {
		if len(header.Extra) != params.ApricotPhase3ExtraDataSize {
			return fmt.Errorf("expected extra-data field to be: %d, but found %d", params.ApricotPhase3ExtraDataSize, len(header.Extra))
		}
		// Verify baseFee and rollupWindow encoding as part of header verification
		expectedRollupWindowBytes, expectedBaseFee, err := self.CalcBaseFee(chain.Config(), parent, header.Time)
		if err != nil {
			return fmt.Errorf("failed to calculate base fee: %w", err)
		}
		if !bytes.Equal(expectedRollupWindowBytes, header.Extra) {
			return fmt.Errorf("expected rollup window bytes: %x, found %x", expectedRollupWindowBytes, header.Extra)
		}
		if header.BaseFee == nil {
			return errors.New("expected baseFee to be non-nil")
		}
		if header.BaseFee.Cmp(expectedBaseFee) != 0 {
			return fmt.Errorf("expected base fee (%d), found (%d)", expectedBaseFee, header.BaseFee)
		}

		if chain.Config().IsApricotPhase4(new(big.Int).SetUint64(header.Time)) {
			expectedBlockGasCost := self.CalcBlockGasCost(chain.Config(), parent, header.Time)
			if header.BlockGasCost == nil {
				return errors.New("expected blockGasCost to be non-nil")
			}
			if header.BlockGasCost.Cmp(expectedBlockGasCost) != 0 {
				return fmt.Errorf("expected block gas cost (%d), found (%d)", expectedBlockGasCost, header.BlockGasCost)
			}
			// ExtDataGasUsed correctness is checked during syntactic validation
			// (when the validator has access to the block contents)
			if header.ExtDataGasUsed == nil {
				return errors.New("expected extDataGasUsed to be non-nil")
			}
		} else {
			if header.BlockGasCost != nil {
				return fmt.Errorf("invalid blockGasCost before fork: have %d, want <nil>", header.BlockGasCost)
			}
			if header.ExtDataGasUsed != nil {
				return fmt.Errorf("invalid extDataGasUsed before fork: have %d, want <nil>", header.ExtDataGasUsed)
			}
		}
	}

	// Verify the header's timestamp
	if header.Time > uint64(time.Now().Add(allowedFutureBlockTime).Unix()) {
		return consensus.ErrFutureBlock
	}
	//if header.Time <= parent.Time {
	if header.Time < parent.Time() {
		return errInvalidBlockTime
	}
	// Verify that the gas limit is <= 2^63-1
	cap := uint64(0x7fffffffffffffff)
	if header.GasLimit > cap {
		return fmt.Errorf("invalid gasLimit: have %v, max %v", header.GasLimit, cap)
	}
	// Verify that the gasUsed is <= gasLimit
	if header.GasUsed > header.GasLimit {
		return fmt.Errorf("invalid gasUsed: have %d, gasLimit %d", header.GasUsed, header.GasLimit)
	}
	if config := chain.Config(); config.IsApricotPhase1(new(big.Int).SetUint64((header.Time))) {
		if header.GasLimit != params.ApricotPhase1GasLimit {
			return fmt.Errorf("expected gas limit to be %d, but found %d", params.ApricotPhase1GasLimit, header.GasLimit)
		}
	} else {
		// Verify that the gas limit remains within allowed bounds
		diff := int64(parent.GasLimit()) - int64(header.GasLimit)
		if diff < 0 {
			diff *= -1
		}
		limit := parent.GasLimit() / params.GasLimitBoundDivisor

		if uint64(diff) >= limit || header.GasLimit < params.MinGasLimit {
			return fmt.Errorf("invalid gas limit: have %d, want %d += %d", header.GasLimit, parent.GasLimit(), limit)
		}
	}

	// Verify that the block number is parent's +1
	if diff := new(big.Int).Sub(header.Number, parent.Number()); diff.Cmp(big.NewInt(1)) != 0 {
		return consensus.ErrInvalidNumber
	}
	// Verify the engine specific seal securing the block
	return self.VerifySeal(chain, header)
}

func (self *DummyEngine) Author(header *types.Header) (common.Address, error) {
	return header.Coinbase, nil
}

func (self *DummyEngine) VerifyHeader(chain consensus.ChainReader, header *types.Header) error {
	// Short circuit if the header is known, or it's parent not
	number := header.Number.Uint64()
	if chain.GetHeader(header.Hash(), number) != nil {
		return nil
	}
	parent := chain.GetBlock(header.ParentHash, number-1)
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

func (self *DummyEngine) verifyBlockFee(baseFee *big.Int, maxBlockGasFee *big.Int, blockFeeDuration, parent, current uint64, txs []*types.Transaction, receipts []*types.Receipt, extraStateChangeContribution *big.Int) error {
	if parent > current {
		return fmt.Errorf("invalid timestamp (%d) < parent timestamp (%d)", current, parent)
	}
	if baseFee.Cmp(common.Big0) <= 0 {
		return fmt.Errorf("invalid base fee (%d) in apricot phase 4", baseFee)
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
	// Calculate the required amount of gas to pay the block fee
	requiredBlockGasFee := calcBlockFee(maxBlockGasFee, blockFeeDuration, parent, current)

	// Require that the amount of gas purchased by the effective tips within the block, [blockGas],
	// covers at least [requiredBlockGasFee].
	if blockGas.Cmp(requiredBlockGasFee) < 0 {
		return fmt.Errorf("insufficient gas (%d) to cover the block fee (%d) at base fee (%d) (total block fee: %d) (parent: %d, current: %d)", blockGas, requiredBlockGasFee, baseFee, totalBlockFee, parent, current)
	}
	return nil
}

func (self *DummyEngine) Finalize(chain consensus.ChainHeaderReader, block *types.Block, parent *types.Header, state *state.StateDB, receipts []*types.Receipt) error {
	// Perform extra state change while finalizing the block
	var contribution *big.Int
	if self.cb.OnExtraStateChange != nil {
		extraStateChangeContribution, err := self.cb.OnExtraStateChange(block, state)
		if err != nil {
			return err
		}
		contribution = extraStateChangeContribution
	}
	if !self.skipBlockFee && chain.Config().IsApricotPhase4(new(big.Int).SetUint64(block.Time())) {
		if err := self.verifyBlockFee(
			block.BaseFee(),
			ApricotPhase4MaxBlockFee,
			ApricotPhase4BlockGasFeeDuration,
			parent.Time,
			block.Time(),
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
	uncles []*types.Header, receipts []*types.Receipt) (*types.Block, error) {
	var (
		contribution *big.Int
		extraData    []byte
		err          error
	)
	if self.cb.OnFinalizeAndAssemble != nil {
		extraData, contribution, err = self.cb.OnFinalizeAndAssemble(header, state, txs)
		if err != nil {
			return nil, err
		}
	}
	if !self.skipBlockFee && chain.Config().IsApricotPhase4(new(big.Int).SetUint64(header.Time)) {
		if err := self.verifyBlockFee(
			header.BaseFee,
			ApricotPhase4MaxBlockFee,
			ApricotPhase4BlockGasFeeDuration,
			parent.Time,
			header.Time,
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
	return types.NewBlock(
		header, txs, uncles, receipts, new(trie.Trie), extraData,
		chain.Config().IsApricotPhase1(new(big.Int).SetUint64(header.Time)),
	), nil
}

func (self *DummyEngine) CalcDifficulty(chain consensus.ChainHeaderReader, time uint64, parent *types.Header) *big.Int {
	return big.NewInt(1)
}

func (self *DummyEngine) APIs(chain consensus.ChainHeaderReader) (res []rpc.API) {
	res = nil
	if self.cb.OnAPIs != nil {
		res = self.cb.OnAPIs(chain)
	}
	return
}

func (self *DummyEngine) Close() error {
	return nil
}

func (self *DummyEngine) MinRequiredTip(chain consensus.ChainHeaderReader, block *types.Block) (*big.Int, error) {
	if self.skipBlockFee || !chain.Config().IsApricotPhase4(new(big.Int).SetUint64(block.Time())) {
		return nil, nil
	}

	if block.Number().Int64() == 0 {
		return nil, nil
	}

	parentHdr := chain.GetHeaderByHash(block.ParentHash())
	if parentHdr == nil {
		return nil, errors.New("parent is nil")
	}

	// Calculate the gas used by atomic transactions
	// Check if nil
	extraStateGasUsed, err := self.cb.ExtraStateGasUsed(block)
	if err != nil {
		return nil, err
	}

	// Calculate the required amount of gas to pay the block fee
	requiredBlockGasFee := calcBlockFee(ApricotPhase4MaxBlockFee, ApricotPhase4BlockGasFeeDuration, parentHdr.Time, block.Time())

	// minTip = requiredBlockFee/blockGasUsage - baseFee
	requiredBlockFee := new(big.Int).Mul(requiredBlockGasFee, block.BaseFee())
	blockGasUsage := new(big.Int).Add(
		new(big.Int).SetUint64(block.GasUsed()),
		new(big.Int).SetUint64(extraStateGasUsed),
	)
	averageGasPrice := new(big.Int).Div(requiredBlockFee, blockGasUsage)
	return new(big.Int).Sub(averageGasPrice, block.BaseFee()), nil
}

func (self *DummyEngine) CalcBlockGasCost(config *params.ChainConfig, parent *types.Block, timestamp uint64) *big.Int {
	// TODO: return error?
	if self.skipBlockFee || !config.IsApricotPhase4(new(big.Int).SetUint64(timestamp)) {
		return nil
	}
	return calcBlockFee(ApricotPhase4MaxBlockFee, ApricotPhase4BlockGasFeeDuration, parent.Time(), timestamp)
}

func (self *DummyEngine) CalcExtDataGasUsed(config *params.ChainConfig, block *types.Block) (*big.Int, error) {
	// TODO: return error?
	if self.skipBlockFee || !config.IsApricotPhase4(new(big.Int).SetUint64(block.Time())) {
		return nil, nil
	}
	// TODO: check if nil
	// TODO: change to return big.Int
	gasUsed, err := self.cb.ExtraStateGasUsed(block)
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetUint64(gasUsed), nil
}
