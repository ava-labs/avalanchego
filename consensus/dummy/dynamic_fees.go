// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
)

var (
	MaxBaseFee          = big.NewInt(params.ApricotPhase3MaxBaseFee)
	MinBaseFee          = big.NewInt(params.ApricotPhase3MinBaseFee)
	TargetGas    uint64 = 10_000_000
	BlockGasFee  uint64 = 1_000_000
	rollupWindow uint64 = 10
)

// CalcBaseFee takes the previous header and the timestamp of its child block
// and calculates the expected base fee as well as the encoding of the past
// pricing information for the child block.
// CalcBaseFee should only be called if [timestamp] >= [config.ApricotPhase3Timestamp]
func CalcBaseFee(config *params.ChainConfig, parent *types.Header, timestamp uint64) ([]byte, *big.Int, error) {
	// If the current block is the first EIP-1559 block, or it is the genesis block
	// return the initial slice and initial base fee.
	if !config.IsApricotPhase3(new(big.Int).SetUint64(parent.Time)) || parent.Number.Cmp(common.Big0) == 0 {
		initialSlice := make([]byte, params.ApricotPhase3ExtraDataSize)
		initialBaseFee := big.NewInt(params.ApricotPhase3InitialBaseFee)
		return initialSlice, initialBaseFee, nil
	}
	if len(parent.Extra) != params.ApricotPhase3ExtraDataSize {
		return nil, nil, fmt.Errorf("expected length of parent extra data to be %d, but found %d", params.ApricotPhase3ExtraDataSize, len(parent.Extra))
	}

	if timestamp < parent.Time {
		return nil, nil, fmt.Errorf("cannot calculate base fee for timestamp (%d) prior to parent timestamp (%d)", timestamp, parent.Time)
	}
	roll := timestamp - parent.Time

	// roll the window over by the difference between the timestamps to generate
	// the new rollup window.
	newRollupWindow, err := rollLongWindow(parent.Extra, int(roll))
	if err != nil {
		return nil, nil, err
	}

	var (
		parentGasTarget          = TargetGas
		parentGasTargetBig       = new(big.Int).SetUint64(parentGasTarget)
		baseFeeChangeDenominator = new(big.Int).SetUint64(params.BaseFeeChangeDenominator)
		baseFee                  = new(big.Int).Set(parent.BaseFee)
	)

	// Add in the gas used by the parent block in the correct place
	// If the parent consumed gas within the rollup window, add the consumed
	// gas in.
	if roll < rollupWindow {
		addedGas, overflow := math.SafeAdd(parent.GasUsed, BlockGasFee)
		if overflow {
			addedGas = math.MaxUint64
		}
		slot := rollupWindow - 1 - roll
		start := slot * wrappers.LongLen
		updateLongWindow(newRollupWindow, start, addedGas)
	}

	// Calculate the amount of gas consumed within the rollup window.
	totalGas := sumLongWindow(newRollupWindow, int(rollupWindow))

	if totalGas == parentGasTarget {
		return newRollupWindow, baseFee, nil
	}

	if totalGas > parentGasTarget {
		// If the parent block used more gas than its target, the baseFee should increase.
		gasUsedDelta := new(big.Int).SetUint64(totalGas - parentGasTarget)
		x := new(big.Int).Mul(parent.BaseFee, gasUsedDelta)
		y := x.Div(x, parentGasTargetBig)
		baseFeeDelta := math.BigMax(
			x.Div(y, baseFeeChangeDenominator),
			common.Big1,
		)

		// Gas price is increasing, so ensure it does not increase past the maximum
		baseFee.Add(baseFee, baseFeeDelta)
		baseFee = selectBigWithinBounds(MinBaseFee, baseFee, MaxBaseFee)
	} else {
		// Otherwise if the parent block used less gas than its target, the baseFee should decrease.
		gasUsedDelta := new(big.Int).SetUint64(parentGasTarget - totalGas)
		x := new(big.Int).Mul(parent.BaseFee, gasUsedDelta)
		y := x.Div(x, parentGasTargetBig)
		baseFeeDelta := math.BigMax(
			x.Div(y, baseFeeChangeDenominator),
			common.Big1,
		)

		// If [roll] is greater than [rollupWindow], apply the state transition to the base fee to account
		// for the interval during which no blocks were produced.
		// We use roll/rollupWindow, so that the transition is applied for every [rollupWindow] seconds
		// that has elapsed between the parent and this block.
		if roll > rollupWindow {
			// Note: roll/rollupWindow must be greater than 1 since we've checked that roll > rollupWindow
			baseFeeDelta = baseFeeDelta.Mul(baseFeeDelta, new(big.Int).SetUint64(roll/rollupWindow))
		}
		baseFee = selectBigWithinBounds(MinBaseFee, baseFee.Sub(baseFee, baseFeeDelta), MaxBaseFee)
	}

	return newRollupWindow, baseFee, nil
}

// selectBigWithinBounds returns [value] if it is within the bounds:
// lowerBound <= value <= upperBound or the bound at either end if [value]
// is outside of the defined boundaries.
func selectBigWithinBounds(lowerBound, value, upperBound *big.Int) *big.Int {
	switch {
	case value.Cmp(lowerBound) < 0:
		return new(big.Int).Set(lowerBound)
	case value.Cmp(upperBound) > 0:
		return new(big.Int).Set(upperBound)
	default:
		return value
	}
}

// rollWindow rolls the longs within [consumptionWindow] over by [roll] places.
// For example, if there are 4 longs encoded in a 32 byte slice, rollWindow would
// have the following effect:
// Original:
// [1, 2, 3, 4]
// Roll = 0
// [1, 2, 3, 4]
// Roll = 1
// [2, 3, 4, 0]
// Roll = 2
// [3, 4, 0, 0]
// Roll = 3
// [4, 0, 0, 0]
// Roll >= 4
// [0, 0, 0, 0]
// Assumes that [roll] is greater than or equal to 0
func rollWindow(consumptionWindow []byte, size, roll int) ([]byte, error) {
	if len(consumptionWindow)%size != 0 {
		return nil, fmt.Errorf("expected consumption window length (%d) to be a multiple of size (%d)", len(consumptionWindow), size)
	}

	// Note: make allocates a zeroed array, so we are guaranteed
	// that what we do not copy into, will be set to 0
	res := make([]byte, len(consumptionWindow))
	bound := roll * size
	if bound > len(consumptionWindow) {
		return res, nil
	}
	copy(res[:], consumptionWindow[roll*size:])
	return res, nil
}

func rollLongWindow(consumptionWindow []byte, roll int) ([]byte, error) {
	// Passes in [wrappers.LongLen] as the size of the individual value to be rolled over
	// so that it can be used to roll an array of long values.
	return rollWindow(consumptionWindow, wrappers.LongLen, roll)
}

// sumLongWindow sums [numLongs] encoded in [window]. Assumes that the length of [window]
// is sufficient to contain [numLongs] or else this function panics.
// If an overflow occurs, while summing the contents, the maximum uint64 value is returned.
func sumLongWindow(window []byte, numLongs int) uint64 {
	var (
		sum      uint64 = 0
		overflow bool
	)
	for i := 0; i < numLongs; i++ {
		// If an overflow occurs while summing the elements of the window, return the maximum
		// uint64 value immediately.
		sum, overflow = math.SafeAdd(sum, binary.BigEndian.Uint64(window[wrappers.LongLen*i:]))
		if overflow {
			return math.MaxUint64
		}
	}
	return sum
}

// updateLongWindow adds [gasConsumed] in at index within [window].
// Assumes that [index] has already been validated.
// If an overflow occurs, the maximum uint64 value is used.
func updateLongWindow(window []byte, start uint64, gasConsumed uint64) {
	prevGasConsumed := binary.BigEndian.Uint64(window[start:])

	totalGasConsumed, overflow := math.SafeAdd(prevGasConsumed, gasConsumed)
	if overflow {
		totalGasConsumed = math.MaxUint64
	}
	binary.BigEndian.PutUint64(window[start:], totalGasConsumed)
}
