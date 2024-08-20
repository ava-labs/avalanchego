// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

type ValidatorState struct {
	Current                  Gas
	Target                   Gas
	Capacity                 Gas
	Excess                   Gas
	MinFee                   GasPrice
	ExcessConversionConstant Gas
}

func (v ValidatorState) CalculateContinuousFee(seconds uint64) uint64 {
	if v.Current == v.Target {
		return uint64(CalculateGasPrice(v.MinFee, v.Excess, v.ExcessConversionConstant))
	}

	var totalFee uint64
	if v.Current < v.Target {
		secondsTillExcessIsZero := uint64(v.Excess / (v.Target - v.Current))

		if secondsTillExcessIsZero < seconds {
			totalFee += uint64(v.MinFee) * (seconds - secondsTillExcessIsZero)
			seconds = secondsTillExcessIsZero
		}
	}

	x := v.Excess
	for i := uint64(0); i < seconds; i++ {
		if v.Current < v.Target {
			x = x.SubPerSecond(v.Target-v.Current, 1)
		} else {
			x = x.AddPerSecond(v.Current-v.Target, 1)
		}

		totalFee += uint64(CalculateGasPrice(v.MinFee, x, v.ExcessConversionConstant))
	}

	return totalFee
}

// Returns the first number n where CalculateContinuousFee(n) >= balance
func (v ValidatorState) CalculateTimeTillContinuousFee(balance uint64) uint64 {
	var (
		totalFee uint64
		i        uint64

		x = v.Excess
	)
	for {
		i += 1

		if v.Current < v.Target {
			x = x.SubPerSecond(v.Target-v.Current, 1)
		} else {
			x = x.AddPerSecond(v.Current-v.Target, 1)
		}

		totalFee += uint64(CalculateGasPrice(v.MinFee, x, v.ExcessConversionConstant))
		if totalFee >= balance {
			return i
		}
	}
}
