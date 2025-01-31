// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"fmt"
	"math/big"

	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
)

// CalcBaseFee takes the previous header and the timestamp of its child block
// and calculates the expected base fee as well as the encoding of the past
// pricing information for the child block.
func CalcBaseFee(config *params.ChainConfig, feeConfig commontype.FeeConfig, parent *types.Header, timestamp uint64) ([]byte, *big.Int, error) {
	// If the current block is the first EIP-1559 block, or it is the genesis block
	// return the initial slice and initial base fee.
	isSubnetEVM := config.IsSubnetEVM(parent.Time)
	if !isSubnetEVM || parent.Number.Cmp(common.Big0) == 0 {
		initialSlice := (&DynamicFeeWindow{}).Bytes()
		return initialSlice, feeConfig.MinBaseFee, nil
	}

	dynamicFeeWindow, err := ParseDynamicFeeWindow(parent.Extra)
	if err != nil {
		return nil, nil, err
	}

	// start off with parent's base fee
	baseFee := new(big.Int).Set(parent.BaseFee)
	baseFeeChangeDenominator := feeConfig.BaseFeeChangeDenominator

	parentGasTargetBig := feeConfig.TargetGas
	parentGasTarget := parentGasTargetBig.Uint64()

	// Compute the new state of the gas rolling window.
	dynamicFeeWindow.Add(parent.GasUsed)

	if timestamp < parent.Time {
		return nil, nil, fmt.Errorf("cannot calculate base fee for timestamp %d prior to parent timestamp %d", timestamp, parent.Time)
	}
	roll := timestamp - parent.Time

	// roll the window over by the difference between the timestamps to generate
	// the new rollup window.
	dynamicFeeWindow.Shift(roll)
	dynamicFeeWindowBytes := dynamicFeeWindow.Bytes()

	// Calculate the amount of gas consumed within the rollup window.
	totalGas := dynamicFeeWindow.Sum()
	if totalGas == parentGasTarget {
		return dynamicFeeWindowBytes, baseFee, nil
	}

	num := new(big.Int)

	if totalGas > parentGasTarget {
		// If the parent block used more gas than its target, the baseFee should increase.
		num.SetUint64(totalGas - parentGasTarget)
		num.Mul(num, parent.BaseFee)
		num.Div(num, parentGasTargetBig)
		num.Div(num, baseFeeChangeDenominator)
		baseFeeDelta := math.BigMax(num, common.Big1)

		baseFee.Add(baseFee, baseFeeDelta)
	} else {
		// Otherwise if the parent block used less gas than its target, the baseFee should decrease.
		num.SetUint64(parentGasTarget - totalGas)
		num.Mul(num, parent.BaseFee)
		num.Div(num, parentGasTargetBig)
		num.Div(num, baseFeeChangeDenominator)
		baseFeeDelta := math.BigMax(num, common.Big1)

		// If [roll] is greater than [rollupWindow], apply the state transition to the base fee to account
		// for the interval during which no blocks were produced.
		// We use roll/rollupWindow, so that the transition is applied for every [rollupWindow] seconds
		// that has elapsed between the parent and this block.
		if roll > params.RollupWindow {
			// Note: roll/params.RollupWindow must be greater than 1 since we've checked that roll > params.RollupWindow
			baseFeeDelta = new(big.Int).Mul(baseFeeDelta, new(big.Int).SetUint64(roll/params.RollupWindow))
		}
		baseFee.Sub(baseFee, baseFeeDelta)
	}

	baseFee = selectBigWithinBounds(feeConfig.MinBaseFee, baseFee, nil)

	return dynamicFeeWindowBytes, baseFee, nil
}

// EstimateNextBaseFee attempts to estimate the next base fee based on a block with [parent] being built at
// [timestamp].
// If [timestamp] is less than the timestamp of [parent], then it uses the same timestamp as parent.
// Warning: This function should only be used in estimation and should not be used when calculating the canonical
// base fee for a subsequent block.
func EstimateNextBaseFee(config *params.ChainConfig, feeConfig commontype.FeeConfig, parent *types.Header, timestamp uint64) ([]byte, *big.Int, error) {
	if timestamp < parent.Time {
		timestamp = parent.Time
	}
	return CalcBaseFee(config, feeConfig, parent, timestamp)
}

// selectBigWithinBounds returns [value] if it is within the bounds:
// lowerBound <= value <= upperBound or the bound at either end if [value]
// is outside of the defined boundaries.
func selectBigWithinBounds(lowerBound, value, upperBound *big.Int) *big.Int {
	switch {
	case lowerBound != nil && value.Cmp(lowerBound) < 0:
		return new(big.Int).Set(lowerBound)
	case upperBound != nil && value.Cmp(upperBound) > 0:
		return new(big.Int).Set(upperBound)
	default:
		return value
	}
}

// calcBlockGasCost calculates the required block gas cost. If [parentTime]
// > [currentTime], the timeElapsed will be treated as 0.
func calcBlockGasCost(
	targetBlockRate uint64,
	minBlockGasCost *big.Int,
	maxBlockGasCost *big.Int,
	blockGasCostStep *big.Int,
	parentBlockGasCost *big.Int,
	parentTime, currentTime uint64,
) *big.Int {
	// Handle Subnet EVM boundary by returning the minimum value as the boundary.
	if parentBlockGasCost == nil {
		return new(big.Int).Set(minBlockGasCost)
	}

	// Treat an invalid parent/current time combination as 0 elapsed time.
	var timeElapsed uint64
	if parentTime <= currentTime {
		timeElapsed = currentTime - parentTime
	}

	var blockGasCost *big.Int
	if timeElapsed < targetBlockRate {
		blockGasCostDelta := new(big.Int).Mul(blockGasCostStep, new(big.Int).SetUint64(targetBlockRate-timeElapsed))
		blockGasCost = new(big.Int).Add(parentBlockGasCost, blockGasCostDelta)
	} else {
		blockGasCostDelta := new(big.Int).Mul(blockGasCostStep, new(big.Int).SetUint64(timeElapsed-targetBlockRate))
		blockGasCost = new(big.Int).Sub(parentBlockGasCost, blockGasCostDelta)
	}

	blockGasCost = selectBigWithinBounds(minBlockGasCost, blockGasCost, maxBlockGasCost)
	if !blockGasCost.IsUint64() {
		blockGasCost = new(big.Int).SetUint64(math.MaxUint64)
	}
	return blockGasCost
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
