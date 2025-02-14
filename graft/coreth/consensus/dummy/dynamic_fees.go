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

	ApricotPhase4BaseFeeChangeDenominator = new(big.Int).SetUint64(params.ApricotPhase4BaseFeeChangeDenominator)
	ApricotPhase5BaseFeeChangeDenominator = new(big.Int).SetUint64(params.ApricotPhase5BaseFeeChangeDenominator)

	errEstimateBaseFeeWithoutActivation = errors.New("cannot estimate base fee for chain without apricot phase 3 scheduled")
)

// CalcBaseFee takes the previous header and the timestamp of its child block
// and calculates the expected base fee as well as the encoding of the past
// pricing information for the child block.
// CalcBaseFee should only be called if [timestamp] >= [config.ApricotPhase3Timestamp]
func CalcBaseFee(config *params.ChainConfig, parent *types.Header, timestamp uint64) ([]byte, *big.Int, error) {
	// If the current block is the first EIP-1559 block, or it is the genesis block
	// return the initial slice and initial base fee.
	var (
		isApricotPhase3 = config.IsApricotPhase3(parent.Time)
		isApricotPhase4 = config.IsApricotPhase4(parent.Time)
		isApricotPhase5 = config.IsApricotPhase5(parent.Time)
		isEtna          = config.IsEtna(parent.Time)
	)
	if !isApricotPhase3 || parent.Number.Cmp(common.Big0) == 0 {
		initialSlice := (&DynamicFeeWindow{}).Bytes()
		initialBaseFee := big.NewInt(params.ApricotPhase3InitialBaseFee)
		return initialSlice, initialBaseFee, nil
	}

	dynamicFeeWindow, err := ParseDynamicFeeWindow(parent.Extra)
	if err != nil {
		return nil, nil, err
	}

	if timestamp < parent.Time {
		return nil, nil, fmt.Errorf("cannot calculate base fee for timestamp %d prior to parent timestamp %d", timestamp, parent.Time)
	}
	timeElapsed := timestamp - parent.Time

	// If AP5, use a less responsive BaseFeeChangeDenominator and a higher gas
	// block limit
	var (
		baseFee                         = new(big.Int).Set(parent.BaseFee)
		baseFeeChangeDenominator        = ApricotPhase4BaseFeeChangeDenominator
		parentGasTarget          uint64 = params.ApricotPhase3TargetGas
	)
	if isApricotPhase5 {
		baseFeeChangeDenominator = ApricotPhase5BaseFeeChangeDenominator
		parentGasTarget = params.ApricotPhase5TargetGas
	}
	parentGasTargetBig := new(big.Int).SetUint64(parentGasTarget)

	// Add in parent's consumed gas
	var blockGasCost, parentExtraStateGasUsed uint64
	switch {
	case isApricotPhase5:
		// blockGasCost has been removed in AP5, so it is left as 0.

		// At the start of a new network, the parent
		// may not have a populated ExtDataGasUsed.
		if parent.ExtDataGasUsed != nil {
			parentExtraStateGasUsed = parent.ExtDataGasUsed.Uint64()
		}
	case isApricotPhase4:
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
		// If timeElapsed is greater than [params.RollupWindow], apply the
		// state transition to the base fee to account for the interval during
		// which no blocks were produced.
		//
		// We use timeElapsed/params.RollupWindow, so that the transition is
		// applied for every [params.RollupWindow] seconds that has elapsed
		// between the parent and this block.
		if timeElapsed > params.RollupWindow {
			// Note: timeElapsed/params.RollupWindow must be at least 1 since
			// we've checked that timeElapsed > params.RollupWindow
			baseFeeDelta = new(big.Int).Mul(baseFeeDelta, new(big.Int).SetUint64(timeElapsed/params.RollupWindow))
		}
		baseFee.Sub(baseFee, baseFeeDelta)
	}

	// Ensure that the base fee does not increase/decrease outside of the bounds
	switch {
	case isEtna:
		baseFee = selectBigWithinBounds(EtnaMinBaseFee, baseFee, MaxUint256)
	case isApricotPhase5:
		baseFee = selectBigWithinBounds(ApricotPhase4MinBaseFee, baseFee, MaxUint256)
	case isApricotPhase4:
		baseFee = selectBigWithinBounds(ApricotPhase4MinBaseFee, baseFee, ApricotPhase4MaxBaseFee)
	default:
		baseFee = selectBigWithinBounds(ApricotPhase3MinBaseFee, baseFee, ApricotPhase3MaxBaseFee)
	}

	return dynamicFeeWindowBytes, baseFee, nil
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
	_, baseFee, err := CalcBaseFee(config, parent, timestamp)
	return baseFee, err
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
