package fee

import (
	"fmt"

	"github.com/holiman/uint256"
)

type ValidatorState struct {
	Current  Gas
	Target   Gas
	Capacity Gas
	Excess   Gas
	MinFee   GasPrice
	K        Gas
}

func (v ValidatorState) CurrentFeeRate() GasPrice {
	return v.MinFee.MulExp(v.Excess, v.K)
}

func (v ValidatorState) CalculateContinuousFee(seconds uint64) uint64 {
	if v.Current == v.Target {
		return uint64(v.MinFee.MulExp(v.Excess, v.K)) * seconds
	}

	uint256.NewInt(uint64(v.Excess))

	var totalFee uint64
	if v.Current < v.Target {
		secondsTillExcessIsZero := uint64(v.Excess / (v.Target - v.Current))

		if secondsTillExcessIsZero < seconds {
			totalFee += uint64(v.MinFee) * (seconds - secondsTillExcessIsZero)
			seconds = secondsTillExcessIsZero
		}
	}

	x := v.Excess
	for i := uint64(1); i <= seconds; i++ {
		if v.Current < v.Target {
			x = x.SubPerSecond(v.Target-v.Current, 1)
		} else {
			x = x.AddPerSecond(v.Current-v.Target, 1)
		}

		if x == 0 {
			totalFee += uint64(v.MinFee)
			continue
		}

		totalFee += uint64(v.MinFee.MulExp(x, v.K))
	}

	return totalFee
}

// Returns the first number n where CalculateContinuousFee(n) >= balance
func (v ValidatorState) CalculateTimeTillContinuousFee(balance uint64) (uint64, uint64) {
	// Lower bound can be derived from [MinFee].
	n := balance / uint64(v.MinFee)
	interval := n

	numIters := 0
	for {
		fmt.Printf("n=%d", n)
		feeAtN := v.CalculateContinuousFee(n)
		feeBeforeN := v.CalculateContinuousFee(n - 1)
		if feeAtN == balance {
			return n, uint64(numIters)
		}

		if feeAtN > balance && feeBeforeN < balance {
			return n, uint64(numIters)
		}

		if feeAtN > balance {
			if interval > 1 {
				interval /= 2
			}
			n -= interval
		}

		if feeAtN < balance {
			n += interval
		}

		numIters++
	}
}
