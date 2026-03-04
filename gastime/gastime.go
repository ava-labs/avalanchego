// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package gastime measures time based on the consumption of gas.
package gastime

import (
	"math"
	"time"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/libevm/core/types"
	"github.com/holiman/uint256"

	"github.com/ava-labs/strevm/hook"
	"github.com/ava-labs/strevm/intmath"
	"github.com/ava-labs/strevm/proxytime"
)

// Time represents an instant in time, its passage measured in [gas.Gas]
// consumption. It is not thread safe nor is the zero value valid.
//
// In addition to the passage of time, it also tracks excess consumption above a
// target, as described in [ACP-194] as a "continuous" version of [ACP-176].
//
// Copying a Time, either directly or by dereferencing a pointer, will result in
// undefined behaviour. Use [Time.Clone] instead as it reestablishes internal
// invariants.
//
// [ACP-176]: https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/176-dynamic-evm-gas-limit-and-price-discovery-updates
// [ACP-194]: https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-streaming-asynchronous-execution
type Time struct {
	TimeMarshaler
}

// makeTime is a constructor shared by [New] and [Time.Clone].
func makeTime(t *proxytime.Time[gas.Gas], target, excess gas.Gas) *Time {
	tm := &Time{
		TimeMarshaler: TimeMarshaler{
			Time:   t,
			target: target,
			excess: excess,
		},
	}
	tm.establishInvariants()
	return tm
}

func (tm *Time) establishInvariants() {
	tm.Time.SetRateInvariants(&tm.target, &tm.excess)
}

// New returns a new [Time], derived from a [time.Time]. The consumption of
// `target` * [TargetToRate] units of [gas.Gas] is equivalent to a tick of 1
// second. Targets are clamped to the range [[MinTarget], [MaxTarget]].
func New(at time.Time, target, startingExcess gas.Gas) *Time {
	target = clampTarget(target)
	tm := proxytime.Of[gas.Gas](at)
	// [proxytime.Time.SetRate] is documented as never returning an error when
	// no invariants have been registered.
	_ = tm.SetRate(rateOf(target))
	return makeTime(tm, target, startingExcess)
}

// SubSecond scales the value returned by [hook.Points.SubSecondBlockTime] to
// reflect the given gas rate.
func SubSecond(hooks hook.Points, hdr *types.Header, rate gas.Gas) gas.Gas {
	// [hook.Points.SubSecondBlockTime] is required to return values in
	// [0,second). The lower bound guarantees that the conversion to unsigned
	// [gas.Gas] is safe while the upper bound guarantees that the mul-div
	// result can't overflow so we don't have to check the error.
	g, _, _ := intmath.MulDivCeil(
		gas.Gas(hooks.SubSecondBlockTime(hdr)), //nolint:gosec // See above
		rate,
		gas.Gas(time.Second),
	)
	return g
}

// TargetToRate is the ratio between [Time.Target] and [proxytime.Time.Rate].
const TargetToRate = 2

// TargetToExcessScaling is the ratio between [Time.Target] and the reciprocal
// of the [Time.Excess] coefficient used in calculating [Time.Price]. In
// [ACP-176] this is the K variable.
//
// [ACP-176]: https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/176-dynamic-evm-gas-limit-and-price-discovery-updates
const TargetToExcessScaling = 87

// MinTarget is the minimum allowable [Time.Target] to avoid division by zero.
// Values below this are silently clamped.
const MinTarget = gas.Gas(1)

// MaxTarget is the maximum allowable [Time.Target] to avoid overflows of the
// associated [proxytime.Time.Rate]. Values above this are silently clamped.
const MaxTarget = gas.Gas(math.MaxUint64 / TargetToRate)

func rateOf(target gas.Gas) gas.Gas { return target * TargetToRate }
func clampTarget(t gas.Gas) gas.Gas { return min(max(t, MinTarget), MaxTarget) }
func roundRate(r gas.Gas) gas.Gas   { return (r / TargetToRate) * TargetToRate }

// SafeRateOfTarget returns the corresponding rate for the given gas target,
// after clamping it to the allowable range.
//
// The argument is clamped to the range [[MinTarget], [MaxTarget]] and
// multiplied by [TargetToRate].
func SafeRateOfTarget(target gas.Gas) gas.Gas {
	return rateOf(clampTarget(target))
}

// Clone returns a deep copy of the time.
func (tm *Time) Clone() *Time {
	// [proxytime.Time.Clone] explicitly does NOT clone the rate invariants, so
	// we reestablish them as if we were constructing a new instance.
	return makeTime(tm.Time.Clone(), tm.target, tm.excess)
}

// Target returns the `T` parameter of ACP-176.
func (tm *Time) Target() gas.Gas {
	return tm.target
}

// Excess returns the `x` variable of ACP-176.
func (tm *Time) Excess() gas.Gas {
	return tm.excess
}

// Price returns the price of a unit of gas, i.e. the "base fee".
func (tm *Time) Price() gas.Price {
	return gas.CalculatePrice(1 /* M */, tm.excess, tm.excessScalingFactor())
}

// excessScalingFactor returns the K variable of ACP-103/176, i.e. 87*T, capped
// at [math.MaxUint64].
func (tm *Time) excessScalingFactor() gas.Gas {
	const overflowThreshold = math.MaxUint64 / TargetToExcessScaling
	if tm.target > overflowThreshold {
		return math.MaxUint64
	}
	return TargetToExcessScaling * tm.target
}

// BaseFee is equivalent to [Time.Price], returning the result as a uint256 for
// compatibility with geth/libevm objects.
func (tm *Time) BaseFee() *uint256.Int {
	return uint256.NewInt(uint64(tm.Price()))
}

// SetRate changes the gas rate per second, rounding down the argument if it is
// not a multiple of [TargetToRate]. See [Time.SetTarget] re potential error(s).
func (tm *Time) SetRate(r gas.Gas) error {
	return tm.TimeMarshaler.SetRate(roundRate(r))
}

// SetTarget changes the target gas consumption per second, clamping the
// argument to the range [[MinTarget], [MaxTarget]]. It returns an error if the
// scaled [Time.Excess] overflows as a result of the scaling.
func (tm *Time) SetTarget(t gas.Gas) error {
	return tm.TimeMarshaler.SetRate(rateOf(clampTarget(t))) // also updates [Time.Target] as it was passed to [proxytime.Time.SetRateInvariants]
}

// Tick is equivalent to [proxytime.Time.Tick] except that it also updates the
// gas excess.
func (tm *Time) Tick(g gas.Gas) {
	tm.Time.Tick(g)

	R, T := tm.Rate(), tm.Target()
	quo, _, _ := intmath.MulDiv(g, R-T, R) // overflow is impossible as (R-T)/R < 1
	tm.excess += quo
}

// FastForwardTo is equivalent to [proxytime.Time.FastForwardTo] except that it
// may also update the gas excess.
func (tm *Time) FastForwardTo(to uint64, toFrac gas.Gas) {
	sec, frac := tm.Time.FastForwardTo(to, toFrac)
	if sec == 0 && frac.Numerator == 0 {
		return
	}

	R, T := tm.Rate(), tm.Target()

	// Excess is reduced by the amount of gas skipped (g), multiplied by T/R.
	// However, to avoid overflow, the implementation needs to be a bit more
	// complicated. The reduction in excess can be calculated as follows (math
	// notation, not code, and ignoring the bounding at zero):
	//
	// s := seconds fast-forwarded (`sec`)
	// f := `frac.Numerator`
	// x := excess
	//
	// dx = -g·T/R
	// = -(sR + f)·T/R
	// = -sR·T/R - fT/R
	// = -sT - fT/R
	//
	// Note that this is equivalent to the ACP reduction of T·dt because dt is
	// equal to s + f/R since `frac.Denominator == R` is a documented invariant.
	// Therefore dx = -(s + f/R)·T, but we separate the terms differently for
	// our implementation.

	// -sT
	if s := gas.Gas(sec); tm.excess/T >= s { // sT <= x; division is safe because T > 0
		tm.excess -= s * T
	} else { // sT > x
		tm.excess = 0
	}

	// -fT/R
	quo, _, _ := intmath.MulDiv(frac.Numerator, T, R) // overflow is impossible as T/R < 1
	tm.excess = intmath.BoundedSubtract(tm.excess, quo, 0)
}
