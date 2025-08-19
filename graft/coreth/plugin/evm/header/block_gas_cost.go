// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"errors"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap4"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap5"
)

var (
	errBaseFeeNil        = errors.New("base fee is nil")
	errBlockGasCostNil   = errors.New("block gas cost is nil")
	errExtDataGasUsedNil = errors.New("extDataGasUsed is nil")
	errNoGasUsed         = errors.New("no gas used")
)

// BlockGasCost calculates the required block gas cost based on the parent
// header and the timestamp of the new block.
// Prior to AP4, the returned block gas cost will be nil.
func BlockGasCost(
	config *extras.ChainConfig,
	parent *types.Header,
	timestamp uint64,
) *big.Int {
	if !config.IsApricotPhase4(timestamp) {
		return nil
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

// EstimateRequiredTip is the estimated tip a transaction would have needed to
// pay to be included in a given block (assuming it paid a tip proportional to
// its gas usage).
//
// In reality, the consensus engine does not enforce a minimum tip on individual
// transactions. The only correctness check performed is that the sum of all
// tips is >= the required block fee.
//
// This function will return nil for all return values prior to Apricot Phase 4.
func EstimateRequiredTip(
	config *extras.ChainConfig,
	header *types.Header,
) (*big.Int, error) {
	extra := customtypes.GetHeaderExtra(header)
	switch {
	case !config.IsApricotPhase4(header.Time):
		return nil, nil
	case header.BaseFee == nil:
		return nil, errBaseFeeNil
	case extra.BlockGasCost == nil:
		return nil, errBlockGasCostNil
	case extra.ExtDataGasUsed == nil:
		return nil, errExtDataGasUsedNil
	}

	// totalGasUsed = GasUsed + ExtDataGasUsed
	totalGasUsed := new(big.Int).SetUint64(header.GasUsed)
	totalGasUsed.Add(totalGasUsed, extra.ExtDataGasUsed)
	if totalGasUsed.Sign() == 0 {
		return nil, errNoGasUsed
	}

	// totalRequiredTips = blockGasCost * baseFee + totalGasUsed - 1
	//
	// We add totalGasUsed - 1 to ensure that the total required tips
	// calculation rounds up.
	totalRequiredTips := new(big.Int)
	totalRequiredTips.Mul(extra.BlockGasCost, header.BaseFee)
	totalRequiredTips.Add(totalRequiredTips, totalGasUsed)
	totalRequiredTips.Sub(totalRequiredTips, common.Big1)

	// estimatedTip = totalRequiredTips / totalGasUsed
	estimatedTip := totalRequiredTips.Div(totalRequiredTips, totalGasUsed)
	return estimatedTip, nil
}
