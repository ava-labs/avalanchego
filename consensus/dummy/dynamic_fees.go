// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"encoding/binary"
	"math/big"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
)

var (
	TargetGas   = uint64(24_000_000)
	MaxGasPrice = new(big.Int).SetUint64(10_000 * params.GWei)
	MinGasPrice = new(big.Int).SetUint64(10 * params.GWei)
	BlockGasFee = uint64(1_000_000)
)

// CalcBaseFee takes the previous header and the timestamp of its child block
// and calculates the expected base fee as well as the encoding of the past
// pricing information for the child block.
func CalcBaseFee(config *params.ChainConfig, parent *types.Header, timestamp uint64) ([]byte, *big.Int) {
	// If the current block is the first EIP-1559 block, or it is the genesis block
	// return the initial slice and initial base fee.
	if !config.IsApricotPhase4(new(big.Int).SetUint64(parent.Time)) || parent.Number.Cmp(common.Big0) == 0 {
		initialSlice := make([]byte, 80)
		initialBaseFee := new(big.Int).SetUint64(params.InitialBaseFee)
		return initialSlice, initialBaseFee
	}

	roll := timestamp - parent.Time
	rollupWindowData := parent.Extra
	// Take the first 80 bytes as the rollup interval of the past 10 seconds
	shortInterval := rollupWindowData[:80]
	// roll the window over by the difference between the timestamps to generate
	// the new rollup window.
	newRollupWindow := rollWindow(shortInterval, 8, int(roll))

	var (
		parentGasTarget          = TargetGas / params.ElasticityMultiplier
		parentGasTargetBig       = new(big.Int).SetUint64(parentGasTarget)
		baseFeeChangeDenominator = new(big.Int).SetUint64(params.BaseFeeChangeDenominator)
		baseFee                  = new(big.Int).Set(parent.BaseFee)
	)

	// Add in the gas used by the parent block in the correct place
	// If the parent consumed gas within the rollup window, add the consumed
	// gas in.
	if roll < 10 {
		slot := 10 - roll
		prevGasConsumed := binary.BigEndian.Uint64(newRollupWindow[slot*8:])
		// Count gas consumed as the previous gas consumed + parent block gas consumed
		// + BlockGasFee to charge for the block itself.
		gasConsumed := prevGasConsumed + parent.GasUsed + BlockGasFee
		binary.BigEndian.PutUint64(newRollupWindow[slot*8:], gasConsumed)
	}

	// Sum the rollup window
	// If there are a large number of blocks in the same 10s window, this will cause more
	// state transitions and push the base fee up more.
	totalGas := uint64(0)
	for i := 0; i < 10; i++ {
		totalGas += binary.BigEndian.Uint64(newRollupWindow[8*i:])
	}

	if totalGas == TargetGas {
		return newRollupWindow, baseFee
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
		baseFee = math.BigMin(baseFee, new(big.Int).Set(MaxGasPrice))
	} else {
		// Otherwise if the parent block used less gas than its target, the baseFee should decrease.
		gasUsedDelta := new(big.Int).SetUint64(parentGasTarget - totalGas)
		x := new(big.Int).Mul(parent.BaseFee, gasUsedDelta)
		y := x.Div(x, parentGasTargetBig)
		baseFeeDelta := x.Div(y, baseFeeChangeDenominator)

		// If [roll] is greater than 10, we want to apply the state transition to the
		// base fee to account for the interval during which no blocks were produced.
		if roll > 10 {
			baseFeeDelta = baseFeeDelta.Mul(baseFeeDelta, new(big.Int).SetUint64(roll/10))
		}
		baseFee = math.BigMax(baseFee.Sub(baseFee, baseFeeDelta), MinGasPrice)
	}

	return newRollupWindow, baseFee
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
func rollWindow(consumptionWindow []byte, size, roll int) []byte {
	if len(consumptionWindow)%size != 0 {
		panic("Expected consumption window to be a multiple of 8")
	}

	// Note: make allocates a zeroed array, so we are guaranteed
	// that what we do not copy into, will be set to 0
	res := make([]byte, len(consumptionWindow))
	bound := roll * size
	if bound > len(consumptionWindow) {
		return res
	}
	copy(res[:], consumptionWindow[roll*size:])
	return res
}
