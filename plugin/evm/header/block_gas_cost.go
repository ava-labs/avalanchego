// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"errors"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/params/extras"
	"github.com/ava-labs/subnet-evm/plugin/evm/blockgascost"
	"github.com/ava-labs/subnet-evm/plugin/evm/customtypes"
)

var (
	errBaseFeeNil      = errors.New("base fee is nil")
	errBlockGasCostNil = errors.New("block gas cost is nil")
	errNoGasUsed       = errors.New("no gas used")
)

// BlockGasCost calculates the required block gas cost based on the parent
// header and the timestamp of the new block.
// Prior to Subnet-EVM, the returned block gas cost will be nil.
func BlockGasCost(
	config *extras.ChainConfig,
	feeConfig commontype.FeeConfig,
	parent *types.Header,
	timestamp uint64,
) *big.Int {
	if !config.IsSubnetEVM(timestamp) {
		return nil
	}
	step := feeConfig.BlockGasCostStep.Uint64()
	// Treat an invalid parent/current time combination as 0 elapsed time.
	//
	// TODO: Does it even make sense to handle this? The timestamp should be
	// verified to ensure this never happens.
	var timeElapsed uint64
	if parent.Time <= timestamp {
		timeElapsed = timestamp - parent.Time
	}
	return new(big.Int).SetUint64(BlockGasCostWithStep(
		feeConfig,
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
	feeConfig commontype.FeeConfig,
	parentCost *big.Int,
	step uint64,
	timeElapsed uint64,
) uint64 {
	if parentCost == nil {
		return feeConfig.MinBlockGasCost.Uint64()
	}

	// [feeConfig.MaxBlockGasCost] is <= MaxUint64, so we know that parentCost is
	// always going to be a valid uint64.
	return blockgascost.BlockGasCost(
		feeConfig,
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
// This function will return nil for all return values prior to SubnetEVM.
func EstimateRequiredTip(
	config *extras.ChainConfig,
	header *types.Header,
) (*big.Int, error) {
	extra := customtypes.GetHeaderExtra(header)
	switch {
	case !config.IsSubnetEVM(header.Time):
		return nil, nil
	case header.BaseFee == nil:
		return nil, errBaseFeeNil
	case extra.BlockGasCost == nil:
		return nil, errBlockGasCostNil
	}

	totalGasUsed := new(big.Int).SetUint64(header.GasUsed)
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
