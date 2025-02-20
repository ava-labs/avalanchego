// (c) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap3"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap4"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap5"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/etna"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
)

// FeeWindowSize is the number of bytes that are used to encode the dynamic fee
// window in the header's Extra field after the Apricot Phase 3 upgrade.
const FeeWindowSize = wrappers.LongLen * ap3.WindowLen

var (
	maxUint256Plus1 = new(big.Int).Lsh(common.Big1, 256)
	maxUint256      = new(big.Int).Sub(maxUint256Plus1, common.Big1)

	ap3MinBaseFee = big.NewInt(ap3.MinBaseFee)
	ap4MinBaseFee = big.NewInt(ap4.MinBaseFee)
	// EtnaMinBaseFee is exported so that it can be modified by tests.
	//
	// TODO: Unexport this.
	EtnaMinBaseFee = big.NewInt(etna.MinBaseFee)

	ap3MaxBaseFee = big.NewInt(ap3.MaxBaseFee)
	ap4MaxBaseFee = big.NewInt(ap4.MaxBaseFee)

	ap3BaseFeeChangeDenominator = big.NewInt(ap3.BaseFeeChangeDenominator)
	ap5BaseFeeChangeDenominator = big.NewInt(ap5.BaseFeeChangeDenominator)

	errInvalidTimestamp                   = errors.New("invalid timestamp")
	errDynamicFeeWindowInsufficientLength = errors.New("insufficient length for dynamic fee window")
)

// baseFeeFromWindow should only be called if `timestamp` >= `config.ApricotPhase3Timestamp`
func baseFeeFromWindow(config *params.ChainConfig, parent *types.Header, timestamp uint64) (*big.Int, error) {
	// If the current block is the first EIP-1559 block, or it is the genesis block
	// return the initial slice and initial base fee.
	if !config.IsApricotPhase3(parent.Time) || parent.Number.Cmp(common.Big0) == 0 {
		return big.NewInt(ap3.InitialBaseFee), nil
	}

	dynamicFeeWindow, err := feeWindow(config, parent, timestamp)
	if err != nil {
		return nil, err
	}

	// If AP5, use a less responsive BaseFeeChangeDenominator and a higher gas
	// block limit
	var (
		isApricotPhase5                 = config.IsApricotPhase5(parent.Time)
		baseFeeChangeDenominator        = ap3BaseFeeChangeDenominator
		parentGasTarget          uint64 = ap3.TargetGas
	)
	if isApricotPhase5 {
		baseFeeChangeDenominator = ap5BaseFeeChangeDenominator
		parentGasTarget = ap5.TargetGas
	}

	// Calculate the amount of gas consumed within the rollup window.
	var (
		baseFee  = new(big.Int).Set(parent.BaseFee)
		totalGas = dynamicFeeWindow.Sum()
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

		// If timeElapsed is greater than [ap3.WindowLen], apply the state
		// transition to the base fee to account for the interval during which
		// no blocks were produced.
		//
		// We use timeElapsed/[ap3.WindowLen], so that the transition is applied
		// for every [ap3.WindowLen] seconds that has elapsed between the parent
		// and this block.
		var (
			timeElapsed    = timestamp - parent.Time
			windowsElapsed = timeElapsed / ap3.WindowLen
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
		baseFee = selectBigWithinBounds(EtnaMinBaseFee, baseFee, maxUint256)
	case isApricotPhase5:
		baseFee = selectBigWithinBounds(ap4MinBaseFee, baseFee, maxUint256)
	case config.IsApricotPhase4(parent.Time):
		baseFee = selectBigWithinBounds(ap4MinBaseFee, baseFee, ap4MaxBaseFee)
	default:
		baseFee = selectBigWithinBounds(ap3MinBaseFee, baseFee, ap3MaxBaseFee)
	}

	return baseFee, nil
}

// feeWindow takes the previous header and the timestamp of its child block and
// calculates the expected fee window.
//
// feeWindow should only be called if timestamp >= config.ApricotPhase3Timestamp
func feeWindow(
	config *params.ChainConfig,
	parent *types.Header,
	timestamp uint64,
) (ap3.Window, error) {
	// If the current block is the first EIP-1559 block, or it is the genesis block
	// return the initial window.
	if !config.IsApricotPhase3(parent.Time) || parent.Number.Cmp(common.Big0) == 0 {
		return ap3.Window{}, nil
	}

	dynamicFeeWindow, err := parseFeeWindow(parent.Extra)
	if err != nil {
		return ap3.Window{}, err
	}

	if timestamp < parent.Time {
		return ap3.Window{}, fmt.Errorf("%w: timestamp %d prior to parent timestamp %d",
			errInvalidTimestamp,
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

		// At the start of a new network, the parent may not have a populated
		// ExtDataGasUsed.
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
		blockGasCost = BlockGasCostWithStep(
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
		blockGasCost = ap3.IntrinsicBlockGas
	}

	// Compute the new state of the gas rolling window.
	dynamicFeeWindow.Add(parent.GasUsed, parentExtraStateGasUsed, blockGasCost)

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

func parseFeeWindow(bytes []byte) (ap3.Window, error) {
	if len(bytes) < FeeWindowSize {
		return ap3.Window{}, fmt.Errorf("%w: expected at least %d bytes but got %d bytes",
			errDynamicFeeWindowInsufficientLength,
			FeeWindowSize,
			len(bytes),
		)
	}

	var window ap3.Window
	for i := range window {
		offset := i * wrappers.LongLen
		window[i] = binary.BigEndian.Uint64(bytes[offset:])
	}
	return window, nil
}

func feeWindowBytes(w ap3.Window) []byte {
	bytes := make([]byte, FeeWindowSize)
	for i, v := range w {
		offset := i * wrappers.LongLen
		binary.BigEndian.PutUint64(bytes[offset:], v)
	}
	return bytes
}
