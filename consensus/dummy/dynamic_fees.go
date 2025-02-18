// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/ap4"
	"github.com/ava-labs/coreth/plugin/evm/header"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
)

const ApricotPhase3BlockGasFee = 1_000_000

var (
	MaxUint256Plus1 = new(big.Int).Lsh(common.Big1, 256)
	MaxUint256      = new(big.Int).Sub(MaxUint256Plus1, common.Big1)

	ApricotPhase3MinBaseFee     = big.NewInt(params.ApricotPhase3MinBaseFee)
	ApricotPhase3MaxBaseFee     = big.NewInt(params.ApricotPhase3MaxBaseFee)
	ApricotPhase4MinBaseFee     = big.NewInt(params.ApricotPhase4MinBaseFee)
	ApricotPhase4MaxBaseFee     = big.NewInt(params.ApricotPhase4MaxBaseFee)
	ApricotPhase3InitialBaseFee = big.NewInt(params.ApricotPhase3InitialBaseFee)
	EtnaMinBaseFee              = big.NewInt(params.EtnaMinBaseFee)

	ApricotPhase3BaseFeeChangeDenominator = new(big.Int).SetUint64(params.ApricotPhase3BaseFeeChangeDenominator)
	ApricotPhase5BaseFeeChangeDenominator = new(big.Int).SetUint64(params.ApricotPhase5BaseFeeChangeDenominator)

	errEstimateBaseFeeWithoutActivation = errors.New("cannot estimate base fee for chain without apricot phase 3 scheduled")
)

// CalcExtraPrefix takes the previous header and the timestamp of its child
// block and calculates the expected extra prefix for the child block.
func CalcExtraPrefix(
	config *params.ChainConfig,
	parent *types.Header,
	timestamp uint64,
) ([]byte, error) {
	switch {
	case config.IsApricotPhase3(timestamp):
		window, err := calcFeeWindow(config, parent, timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate fee window: %w", err)
		}
		return window.Bytes(), nil
	default:
		// Prior to AP3 there was no expected extra prefix.
		return nil, nil
	}
}

// CalcBaseFee takes the previous header and the timestamp of its child block
// and calculates the expected base fee for the child block.
//
// Prior to AP3, the returned base fee will be nil.
func CalcBaseFee(config *params.ChainConfig, parent *types.Header, timestamp uint64) (*big.Int, error) {
	switch {
	case config.IsApricotPhase3(timestamp):
		return calcBaseFeeWithWindow(config, parent, timestamp)
	default:
		// Prior to AP3 the expected base fee is nil.
		return nil, nil
	}
}

// calcBaseFeeWithWindow should only be called if `timestamp` >= `config.ApricotPhase3Timestamp`
func calcBaseFeeWithWindow(config *params.ChainConfig, parent *types.Header, timestamp uint64) (*big.Int, error) {
	// If the current block is the first EIP-1559 block, or it is the genesis block
	// return the initial slice and initial base fee.
	if !config.IsApricotPhase3(parent.Time) || parent.Number.Cmp(common.Big0) == 0 {
		return big.NewInt(params.ApricotPhase3InitialBaseFee), nil
	}

	dynamicFeeWindow, err := calcFeeWindow(config, parent, timestamp)
	if err != nil {
		return nil, err
	}

	// If AP5, use a less responsive BaseFeeChangeDenominator and a higher gas
	// block limit
	var (
		isApricotPhase5                 = config.IsApricotPhase5(parent.Time)
		baseFeeChangeDenominator        = ApricotPhase3BaseFeeChangeDenominator
		parentGasTarget          uint64 = params.ApricotPhase3TargetGas
	)
	if isApricotPhase5 {
		baseFeeChangeDenominator = ApricotPhase5BaseFeeChangeDenominator
		parentGasTarget = params.ApricotPhase5TargetGas
	}

	// Calculate the amount of gas consumed within the rollup window.
	var (
		baseFee  = new(big.Int).Set(parent.BaseFee)
		totalGas = dynamicFeeWindow.Sum()
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

	// Ensure that the base fee does not increase/decrease outside of the bounds
	switch {
	case config.IsEtna(parent.Time):
		baseFee = selectBigWithinBounds(EtnaMinBaseFee, baseFee, MaxUint256)
	case isApricotPhase5:
		baseFee = selectBigWithinBounds(ApricotPhase4MinBaseFee, baseFee, MaxUint256)
	case config.IsApricotPhase4(parent.Time):
		baseFee = selectBigWithinBounds(ApricotPhase4MinBaseFee, baseFee, ApricotPhase4MaxBaseFee)
	default:
		baseFee = selectBigWithinBounds(ApricotPhase3MinBaseFee, baseFee, ApricotPhase3MaxBaseFee)
	}

	return baseFee, nil
}

// calcFeeWindow takes the previous header and the timestamp of its child block
// and calculates the expected fee window.
//
// calcFeeWindow should only be called if timestamp >= config.ApricotPhase3Timestamp
func calcFeeWindow(
	config *params.ChainConfig,
	parent *types.Header,
	timestamp uint64,
) (DynamicFeeWindow, error) {
	// If the current block is the first EIP-1559 block, or it is the genesis block
	// return the initial window.
	if !config.IsApricotPhase3(parent.Time) || parent.Number.Cmp(common.Big0) == 0 {
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

	// Add in parent's consumed gas
	var blockGasCost, parentExtraStateGasUsed uint64
	switch {
	case config.IsApricotPhase5(parent.Time):
		// blockGasCost is not included in the fee window after AP5, so it is
		// left as 0.

		// At the start of a new network, the parent
		// may not have a populated ExtDataGasUsed.
		if parent.ExtDataGasUsed != nil {
			parentExtraStateGasUsed = parent.ExtDataGasUsed.Uint64()
		}
	case config.IsApricotPhase4(parent.Time):
		// The blockGasCost is paid by the effective tips in the block using
		// the block's value of baseFee.
		//
		// Although the child block may be in AP5 here, the blockGasCost is
		// still calculated using the AP4 step. This is different than the
		// actual BlockGasCost calculation used for the child block. This
		// behavior is kept to preserve the original behavior of this function.
		blockGasCost = header.BlockGasCostWithStep(
			parent.BlockGasCost,
			ap4.BlockGasCostStep,
			timeElapsed,
		)

		// On the boundary of AP3 and AP4 or at the start of a new network, the
		// parent may not have a populated ExtDataGasUsed.
		if parent.ExtDataGasUsed != nil {
			parentExtraStateGasUsed = parent.ExtDataGasUsed.Uint64()
		}
	default:
		blockGasCost = ApricotPhase3BlockGasFee
	}

	// Compute the new state of the gas rolling window.
	dynamicFeeWindow.Add(parent.GasUsed, parentExtraStateGasUsed, blockGasCost)

	// roll the window over by the timeElapsed to generate the new rollup
	// window.
	dynamicFeeWindow.Shift(timeElapsed)
	return dynamicFeeWindow, nil
}

// EstimateNextBaseFee attempts to estimate the base fee of a block built at
// `timestamp` on top of `parent`.
//
// If timestamp is before parent.Time or the AP3 activation time, then timestamp
// is set to the maximum of parent.Time and the AP3 activation time.
//
// Warning: This function should only be used in estimation and should not be
// used when calculating the canonical base fee for a block.
func EstimateNextBaseFee(config *params.ChainConfig, parent *types.Header, timestamp uint64) (*big.Int, error) {
	if config.ApricotPhase3BlockTimestamp == nil {
		return nil, errEstimateBaseFeeWithoutActivation
	}

	timestamp = max(timestamp, parent.Time, *config.ApricotPhase3BlockTimestamp)
	return CalcBaseFee(config, parent, timestamp)
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
// This function will return nil for all return values prior to Apricot Phase 4.
func MinRequiredTip(config *params.ChainConfig, header *types.Header) (*big.Int, error) {
	if !config.IsApricotPhase4(header.Time) {
		return nil, nil
	}
	if header.BaseFee == nil {
		return nil, errBaseFeeNil
	}
	if header.BlockGasCost == nil {
		return nil, errBlockGasCostNil
	}
	if header.ExtDataGasUsed == nil {
		return nil, errExtDataGasUsedNil
	}

	// minTip = requiredBlockFee/blockGasUsage
	requiredBlockFee := new(big.Int).Mul(
		header.BlockGasCost,
		header.BaseFee,
	)
	blockGasUsage := new(big.Int).Add(
		new(big.Int).SetUint64(header.GasUsed),
		header.ExtDataGasUsed,
	)
	return new(big.Int).Div(requiredBlockFee, blockGasUsage), nil
}
