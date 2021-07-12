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

var blockGasFee = big.NewInt(1_000_000)

type OnFinalizeCallbackType = func(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, receipts []*types.Receipt, uncles []*types.Header) error
type OnFinalizeAndAssembleCallbackType = func(header *types.Header, state *state.StateDB, txs []*types.Transaction) ([]byte, error)
type OnAPIsCallbackType = func(consensus.ChainHeaderReader) []rpc.API
type OnExtraStateChangeType = func(block *types.Block, statedb *state.StateDB) error

type ConsensusCallbacks struct {
	OnAPIs                OnAPIsCallbackType
	OnFinalize            OnFinalizeCallbackType
	OnFinalizeAndAssemble OnFinalizeAndAssembleCallbackType
	OnExtraStateChange    OnExtraStateChangeType
}

type DummyEngine struct {
	cb *ConsensusCallbacks
}

func NewDummyEngine(cb *ConsensusCallbacks) *DummyEngine {
	return &DummyEngine{
		cb: cb,
	}
}

func NewFaker() *DummyEngine {
	return NewDummyEngine(new(ConsensusCallbacks))
}

var (
	allowedFutureBlockTime = 10 * time.Second // Max time from current time allowed for blocks, before they're considered future blocks
)

var (
	errInvalidBlockTime  = errors.New("timestamp less than parent's")
	errUnclesUnsupported = errors.New("uncles unsupported")
)

// modified from consensus.go
func (self *DummyEngine) verifyHeader(chain consensus.ChainHeaderReader, header, parent *types.Header, uncle bool) error {
	// Ensure that we do not verify an uncle
	if uncle {
		return errUnclesUnsupported
	}
	// Ensure that the header's extra-data section is of a reasonable size
	if uint64(len(header.Extra)) > params.MaximumExtraDataSize {
		return fmt.Errorf("extra-data too long: %d > %d", len(header.Extra), params.MaximumExtraDataSize)
	}
	// Verify the header's timestamp
	if header.Time > uint64(time.Now().Add(allowedFutureBlockTime).Unix()) {
		return consensus.ErrFutureBlock
	}
	//if header.Time <= parent.Time {
	if header.Time < parent.Time {
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
		diff := int64(parent.GasLimit) - int64(header.GasLimit)
		if diff < 0 {
			diff *= -1
		}
		limit := parent.GasLimit / params.GasLimitBoundDivisor

		if uint64(diff) >= limit || header.GasLimit < params.MinGasLimit {
			return fmt.Errorf("invalid gas limit: have %d, want %d += %d", header.GasLimit, parent.GasLimit, limit)
		}
	}
	// Verify baseFee and rollupWindow encoding as part of header verification
	if !chain.Config().IsApricotPhase4(new(big.Int).SetUint64(header.Time)) {
		// Verify BaseFee is not present before EIP-1559
		if header.BaseFee != nil {
			return fmt.Errorf("invalid baseFee before fork: have %d, want <nil>", header.BaseFee)
		}
	} else if false {
		expectedRollupWindowBytes, expectedBaseFee := CalcBaseFee(chain.Config(), parent, header.Time)
		if !bytes.Equal(expectedRollupWindowBytes, header.Extra) {
			return fmt.Errorf("expected rollup window bytes: %x, found %x", expectedRollupWindowBytes, header.Extra)
		}
		if header.BaseFee.Cmp(expectedBaseFee) != 0 {
			return fmt.Errorf("expected base fee (%d), found (%d)", expectedBaseFee, header.BaseFee)
		}
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

func (self *DummyEngine) Finalize(
	chain consensus.ChainHeaderReader, header *types.Header,
	state *state.StateDB, txs []*types.Transaction, receipts []*types.Receipt,
	uncles []*types.Header) error {

	// If Apricot Phase 4 is live, ensure that the transactions in the block have met the block fee.
	if chain.Config().IsApricotPhase4(new(big.Int).SetUint64(header.Time)) {
		blockFeePremium := new(big.Int)
		blockFeeContribution := new(big.Int)
		totalBlockFee := new(big.Int)
		// Calculate the total excess over the base fee that was paid towards the block fee
		for i, receipt := range receipts {
			// Each transaction contributes the excess over the baseFee towards the totalBlockFee
			// This should be equivalent to the sum of the "priority fees" within EIP-1559.
			blockFeePremium = blockFeePremium.Sub(txs[i].GasPrice(), header.BaseFee)
			blockFeeContribution = blockFeeContribution.Mul(blockFeePremium, new(big.Int).SetUint64(receipt.GasUsed))

			totalBlockFee = totalBlockFee.Add(totalBlockFee, blockFeeContribution)
		}
		// TODO factor atomic transactions into the calculation.
		// In order to divide safely, we require that the baseFee must never be 0
		if header.BaseFee.Cmp(common.Big0) <= 0 {
			return fmt.Errorf("invalid base fee (%d) in apricot phase 4", header.BaseFee)
		}
		// Calculate how much gas the [totalBlockFee] would purchase at the price level
		// set by this block.
		blockGas := new(big.Int).Div(totalBlockFee, new(big.Int).Set(header.BaseFee))
		if !blockGas.IsUint64() {
			return fmt.Errorf("calculated block gas was not uint64: %d", blockGas)
		}
		// We require that [blockGas] covers at least [blockGasFee] to ensure that it
		// costs a minimum amount to produce a valid block.
		if blockGas.Cmp(blockGasFee) < 0 {
			return fmt.Errorf("insufficient gas (%d) to cover the block fee (%d) at base fee (%d) (total block fee: %d)", blockGas, blockGasFee, header.BaseFee, totalBlockFee)
		}
	}
	if self.cb.OnFinalize != nil {
		return self.cb.OnFinalize(chain, header, state, txs, receipts, uncles)
	}
	return nil
}

func (self *DummyEngine) FinalizeAndAssemble(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, txs []*types.Transaction,
	uncles []*types.Header, receipts []*types.Receipt) (*types.Block, error) {
	var extdata []byte
	if self.cb.OnFinalizeAndAssemble != nil {
		ret, err := self.cb.OnFinalizeAndAssemble(header, state, txs)
		extdata = ret
		if err != nil {
			return nil, err
		}
	}
	// commit the final state root
	header.Root = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))

	// Header seems complete, assemble into a block and return
	return types.NewBlock(
		header, txs, uncles, receipts, new(trie.Trie), extdata,
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

func (self *DummyEngine) ExtraStateChange(block *types.Block, statedb *state.StateDB) error {
	if self.cb.OnExtraStateChange != nil {
		return self.cb.OnExtraStateChange(block, statedb)
	}
	return nil
}
