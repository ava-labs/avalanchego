// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
)

var (
	maxUint256Plus1                     = new(big.Int).Lsh(common.Big1, 256)
	maxUint256                          = new(big.Int).Sub(maxUint256Plus1, common.Big1)
	errEstimateBaseFeeWithoutActivation = errors.New("cannot estimate base fee for chain without activation")
)

// CalcExtraPrefix takes the previous header and the timestamp of its child
// block and calculates the expected extra prefix for the child block.
func CalcExtraPrefix(
	config *params.ChainConfig,
	feeConfig commontype.FeeConfig,
	parent *types.Header,
	timestamp uint64,
) ([]byte, error) {
	switch {
	case config.IsSubnetEVM(timestamp):
		window, err := calcFeeWindow(config, feeConfig, parent, timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate fee window: %w", err)
		}
		return window.Bytes(), nil
	default:
		// Prior to SubnetEVM there was no expected extra prefix.
		return nil, nil
	}
}

// CalcBaseFee takes the previous header and the timestamp of its child block
// and calculates the expected base fee for the child block.
//
// Prior to SubnetEVM, the returned base fee will be nil.
func CalcBaseFee(config *params.ChainConfig, feeConfig commontype.FeeConfig, parent *types.Header, timestamp uint64) (*big.Int, error) {
	switch {
	case config.IsSubnetEVM(timestamp):
		return calcBaseFeeWithWindow(config, feeConfig, parent, timestamp)
	default:
		// Prior to SubnetEVM the expected base fee is nil.
		return nil, nil
	}
}

// calcBaseFeeWithWindow should only be called if `timestamp` >= `config.SubnetEVMTimestamp`.
func calcBaseFeeWithWindow(config *params.ChainConfig, feeConfig commontype.FeeConfig, parent *types.Header, timestamp uint64) (*big.Int, error) {
	// If the current block is the first EIP-1559 block, or it is the genesis block
	// return the initial slice and initial base fee.
	if !config.IsSubnetEVM(parent.Time) || parent.Number.Cmp(common.Big0) == 0 {
		return big.NewInt(feeConfig.MinBaseFee.Int64()), nil
	}

	dynamicFeeWindow, err := calcFeeWindow(config, feeConfig, parent, timestamp)
	if err != nil {
		return nil, err
	}

	var (
		baseFee                  = new(big.Int).Set(parent.BaseFee)
		baseFeeChangeDenominator = feeConfig.BaseFeeChangeDenominator
		parentGasTarget          = feeConfig.TargetGas.Uint64()
		totalGas                 = dynamicFeeWindow.Sum()
	)

	if totalGas == parentGasTarget {
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

		// If timeElapsed is greater than [params.RollupWindow], apply the
		// state transition to the base fee to account for the interval during
		// which no blocks were produced.
		//
		// We use timeElapsed/params.RollupWindow, so that the transition is
		// applied for every [params.RollupWindow] seconds that has elapsed
		// between the parent and this block.
		var (
			timeElapsed    = timestamp - parent.Time
			windowsElapsed = timeElapsed / params.RollupWindow
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

	baseFee = selectBigWithinBounds(feeConfig.MinBaseFee, baseFee, maxUint256)

	return baseFee, nil
}

// calcFeeWindow takes the previous header and the timestamp of its child block
// and calculates the expected fee window.
//
// calcFeeWindow should only be called if `timestamp“ >= `config.IsSubnetEVM“
func calcFeeWindow(
	config *params.ChainConfig,
	feeConfig commontype.FeeConfig,
	parent *types.Header,
	timestamp uint64,
) (DynamicFeeWindow, error) {
	// If the current block is the first EIP-1559 block, or it is the genesis block
	// return the initial window.
	if !config.IsSubnetEVM(parent.Time) || parent.Number.Cmp(common.Big0) == 0 {
		return DynamicFeeWindow{}, nil
	}

	dynamicFeeWindow, err := ParseDynamicFeeWindow(parent.Extra)
	if err != nil {
		return DynamicFeeWindow{}, err
	}

	if timestamp < parent.Time {
		return DynamicFeeWindow{}, fmt.Errorf("cannot calculate fee window for timestamp %d prior to parent timestamp %d",
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

// EstimateNextBaseFee attempts to estimate the base fee of a block built at
// `timestamp` on top of `parent`.
//
// If timestamp is before parent.Time or the activation time, then timestamp
// is set to the maximum of parent.Time and the activation time.
//
// Warning: This function should only be used in estimation and should not be
// used when calculating the canonical base fee for a block.
func EstimateNextBaseFee(config *params.ChainConfig, feeConfig commontype.FeeConfig, parent *types.Header, timestamp uint64) (*big.Int, error) {
	if config.SubnetEVMTimestamp == nil {
		return nil, errEstimateBaseFeeWithoutActivation
	}

	timestamp = max(timestamp, parent.Time, *config.SubnetEVMTimestamp)
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
