// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/math"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/params/extras"
	"github.com/ava-labs/subnet-evm/plugin/evm/upgrade/subnetevm"
)

var (
	maxUint256Plus1 = new(big.Int).Lsh(common.Big1, 256)
	maxUint256      = new(big.Int).Sub(maxUint256Plus1, common.Big1)

	errInvalidTimestamp = errors.New("invalid timestamp")
)

// baseFeeFromWindow should only be called if `timestamp` >= `config.SubnetEVMTimestamp`
func baseFeeFromWindow(config *extras.ChainConfig, feeConfig commontype.FeeConfig, parent *types.Header, timestamp uint64) (*big.Int, error) {
	// If the current block is the first EIP-1559 block, or it is the genesis block
	// return the initial slice and initial base fee.
	if !config.IsSubnetEVM(parent.Time) || parent.Number.Cmp(common.Big0) == 0 {
		return big.NewInt(feeConfig.MinBaseFee.Int64()), nil
	}

	dynamicFeeWindow, err := feeWindow(config, parent, timestamp)
	if err != nil {
		return nil, err
	}

	// Calculate the amount of gas consumed within the rollup window.
	var (
		baseFee                  = new(big.Int).Set(parent.BaseFee)
		baseFeeChangeDenominator = feeConfig.BaseFeeChangeDenominator
		parentGasTarget          = feeConfig.TargetGas.Uint64()
		totalGas                 = dynamicFeeWindow.Sum()
	)

	if totalGas == parentGasTarget {
		// If the parent block used exactly its target gas, the baseFee stays
		// the same.
		//
		// For legacy reasons, this is true even if the baseFee would have
		// otherwise been clamped to a different range.
		return baseFee, nil
	}

	var (
		num                = new(big.Int)
		parentGasTargetBig = new(big.Int).SetUint64(parentGasTarget)
	)
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

		if timestamp < parent.Time {
			// This should never happen as the fee window calculations should
			// have already failed, but it is kept for clarity.
			return nil, fmt.Errorf("cannot calculate base fee for timestamp %d prior to parent timestamp %d",
				timestamp,
				parent.Time,
			)
		}

		// If timeElapsed is greater than [subnetevm.WindowLen], apply the state
		// transition to the base fee to account for the interval during which
		// no blocks were produced.
		//
		// We use timeElapsed/[subnetevm.WindowLen], so that the transition is applied
		// for every [subnetevm.WindowLen] seconds that has elapsed between the parent
		// and this block.
		var (
			timeElapsed    = timestamp - parent.Time
			windowsElapsed = timeElapsed / subnetevm.WindowLen
		)
		if windowsElapsed > 1 {
			bigWindowsElapsed := new(big.Int).SetUint64(windowsElapsed)
			// Because baseFeeDelta could actually be [common.Big1], we must not
			// modify the existing value of `baseFeeDelta` but instead allocate
			// a new one.
			baseFeeDelta = new(big.Int).Mul(baseFeeDelta, bigWindowsElapsed)
		}
		baseFee.Sub(baseFee, baseFeeDelta)
	}

	// Ensure that the base fee does not increase/decrease outside of the bounds
	baseFee = selectBigWithinBounds(feeConfig.MinBaseFee, baseFee, maxUint256)

	return baseFee, nil
}

// feeWindow takes the previous header and the timestamp of its child block and
// calculates the expected fee window.
//
// feeWindow should only be called if timestamp >= config.SubnetEVMTimestamp
func feeWindow(
	config *extras.ChainConfig,
	parent *types.Header,
	timestamp uint64,
) (subnetevm.Window, error) {
	// If the current block is the first EIP-1559 block, or it is the genesis block
	// return the initial window.
	if !config.IsSubnetEVM(parent.Time) || parent.Number.Cmp(common.Big0) == 0 {
		return subnetevm.Window{}, nil
	}

	dynamicFeeWindow, err := subnetevm.ParseWindow(parent.Extra)
	if err != nil {
		return subnetevm.Window{}, err
	}

	if timestamp < parent.Time {
		return subnetevm.Window{}, fmt.Errorf("%w: timestamp %d prior to parent timestamp %d",
			errInvalidTimestamp,
			timestamp,
			parent.Time,
		)
	}
	timeElapsed := timestamp - parent.Time

	// Compute the new state of the gas rolling window.
	dynamicFeeWindow.Add(parent.GasUsed)

	// roll the window over by the timeElapsed to generate the new rollup
	// window.
	dynamicFeeWindow.Shift(timeElapsed)
	return dynamicFeeWindow, nil
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
