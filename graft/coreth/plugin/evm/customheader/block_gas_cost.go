// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap4"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap5"
)

var (
	ErrInsufficientBlockGas                     = errors.New("insufficient gas to cover the block cost")
	errInvalidExtraStateChangeContribution      = errors.New("invalid extra state change contribution")
	errInvalidBaseFeeApricotPhase4              = errors.New("invalid base fee in apricot phase 4")
	errInvalidRequiredBlockGasCostApricotPhase4 = errors.New("invalid block gas cost in apricot phase 4")
)

// BlockGasCost calculates the required block gas cost based on the parent
// header and the timestamp of the new block.
// Prior to AP4, the returned block gas cost will be nil.
// In Granite, the returned block gas cost will be 0.
func BlockGasCost(
	config *extras.ChainConfig,
	parent *types.Header,
	timestamp uint64,
) *big.Int {
	if !config.IsApricotPhase4(timestamp) {
		return nil
	}
	if config.IsGranite(timestamp) {
		return big.NewInt(0)
	}
	step := uint64(ap4.BlockGasCostStep)
	if config.IsApricotPhase5(timestamp) {
		step = ap5.BlockGasCostStep
	}
	// Treat an invalid parent/current time combination as 0 elapsed time.
	//
	// TODO: Does it even make sense to handle this? The timestamp should be
	// verified to ensure this never happens.
	var timeElapsed uint64
	if parent.Time <= timestamp {
		timeElapsed = timestamp - parent.Time
	}
	return new(big.Int).SetUint64(BlockGasCostWithStep(
		customtypes.GetHeaderExtra(parent).BlockGasCost,
		step,
		timeElapsed,
	))
}

// BlockGasCostWithStep calculates the required block gas cost based on the
// parent cost and the time difference between the parent block and new block.
//
// This is a helper function that allows the caller to manually specify the step
// value to use.
func BlockGasCostWithStep(
	parentCost *big.Int,
	step uint64,
	timeElapsed uint64,
) uint64 {
	// Handle AP3/AP4 boundary by returning the minimum value as the boundary.
	if parentCost == nil {
		return ap4.MinBlockGasCost
	}

	// [ap4.MaxBlockGasCost] is <= MaxUint64, so we know that parentCost is
	// always going to be a valid uint64.
	return ap4.BlockGasCost(
		parentCost.Uint64(),
		step,
		timeElapsed,
	)
}

func VerifyBlockFee(
	baseFee *big.Int,
	requiredBlockGasCost *big.Int,
	txs []*types.Transaction,
	receipts []*types.Receipt,
	extraStateChangeContribution *big.Int,
) error {
	if baseFee == nil || baseFee.Sign() <= 0 {
		return fmt.Errorf("%w: %d", errInvalidBaseFeeApricotPhase4, baseFee)
	}
	if requiredBlockGasCost == nil || !requiredBlockGasCost.IsUint64() {
		return fmt.Errorf("%w: %d", errInvalidRequiredBlockGasCostApricotPhase4, requiredBlockGasCost)
	}
	// If the required block gas cost is 0, we don't need to verify the block fee
	if requiredBlockGasCost.Sign() == 0 {
		return nil
	}

	var (
		gasUsed              = new(big.Int)
		blockFeeContribution = new(big.Int)
		totalBlockFee        = new(big.Int)
	)

	// Add in the external contribution
	if extraStateChangeContribution != nil {
		if extraStateChangeContribution.Cmp(common.Big0) < 0 {
			return fmt.Errorf("%w: %d", errInvalidExtraStateChangeContribution, extraStateChangeContribution)
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
