// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"math/big"

	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/plugin/evm/blockgascost"
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
