// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"errors"
	"math/big"

	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/plugin/evm/blockgascost"
)

var (
	errBaseFeeNil      = errors.New("base fee is nil")
	errBlockGasCostNil = errors.New("block gas cost is nil")
)

// BlockGasCost calculates the required block gas cost based on the parent
// header and the timestamp of the new block.
func BlockGasCost(
	feeConfig commontype.FeeConfig,
	parent *types.Header,
	timestamp uint64,
) uint64 {
	step := feeConfig.BlockGasCostStep.Uint64()
	// Treat an invalid parent/current time combination as 0 elapsed time.
	//
	// TODO: Does it even make sense to handle this? The timestamp should be
	// verified to ensure this never happens.
	var timeElapsed uint64
	if parent.Time <= timestamp {
		timeElapsed = timestamp - parent.Time
	}
	return BlockGasCostWithStep(
		feeConfig,
		parent.BlockGasCost,
		step,
		timeElapsed,
	)
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

// MinRequiredTip is the estimated minimum tip a transaction would have
// needed to pay to be included in a given block (assuming it paid a tip
// proportional to its gas usage). In reality, there is no minimum tip that
// is enforced by the consensus engine and high tip paying transactions can
// subsidize the inclusion of low tip paying transactions. The only
// correctness check performed is that the sum of all tips is >= the
// required block fee.
//
// This function will return nil for all return values prior to Subnet EVM.
func MinRequiredTip(config *params.ChainConfig, header *types.Header) (*big.Int, error) {
	if !config.IsSubnetEVM(header.Time) {
		return nil, nil
	}
	if header.BaseFee == nil {
		return nil, errBaseFeeNil
	}
	if header.BlockGasCost == nil {
		return nil, errBlockGasCostNil
	}

	// minTip = requiredBlockFee/blockGasUsage
	requiredBlockFee := new(big.Int).Mul(
		header.BlockGasCost,
		header.BaseFee,
	)
	return new(big.Int).Div(requiredBlockFee, new(big.Int).SetUint64(header.GasUsed)), nil
}
