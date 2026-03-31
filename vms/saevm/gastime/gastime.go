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

	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/intmath"
	"github.com/ava-labs/avalanchego/vms/saevm/proxytime"
)

//go:generate go run github.com/StephenButtolph/canoto/canoto $GOFILE

// Time represents an instant in time, its passage measured in [gas.Gas]
// consumption. It is not thread safe nor is the zero value valid.
//
// In addition to the passage of time, it also tracks excess consumption above a
// target, as described in [ACP-194] as a "continuous" version of [ACP-176].
//
// Copying a Time, either directly or by dereferencing a pointer, can result in
// undefined behaviour; use [Time.Clone] instead.
//
// [ACP-176]: https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/176-dynamic-evm-gas-limit-and-price-discovery-updates
// [ACP-194]: https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-streaming-asynchronous-execution
//
//nolint:tagliatelle // Can't handle embedded field
type Time struct {
	*proxytime.Time[gas.Gas] `canoto:"pointer,1"`
	target                   gas.Gas `canoto:"uint,2"`
	excess                   gas.Gas `canoto:"uint,3"`
	config                   config  `canoto:"value,4"`

	canotoData canotoData_Time `canoto:"nocopy"`
}

// New returns a new [Time], derived from a [time.Time]. The consumption of
// `target` * [TargetToRate] units of [gas.Gas] is equivalent to a tick of 1
// second.
func New(at time.Time, target, startingExcess gas.Gas, gasPriceConfig hook.GasPriceConfig) (*Time, error) {
	cfg, err := newConfig(gasPriceConfig)
	if err != nil {
		return nil, err
	}

	tm := proxytime.Of[gas.Gas](at)
	target = clampTarget(target)
	tm.SetRate(rateOf(target))

	return &Time{
		Time:   tm,
		target: target,
		excess: startingExcess,
		config: cfg,
	}, nil
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

// DefaultTargetToExcessScaling is the default ratio between gas target and the
// reciprocal of the excess coefficient used in price calculation (K variable in ACP-176).
const DefaultTargetToExcessScaling = 87

// DefaultMinPrice is the default minimum gas price (base fee), i.e. the M
// parameter in ACP-176's price calculation.
const DefaultMinPrice gas.Price = 1

// DefaultGasPriceConfig returns the default [hook.GasPriceConfig] values.
func DefaultGasPriceConfig() hook.GasPriceConfig {
	return hook.GasPriceConfig{
		TargetToExcessScaling: DefaultTargetToExcessScaling,
		MinPrice:              DefaultMinPrice,
		StaticPricing:         false,
	}
}

// MinTarget is the minimum allowable [Time.Target] to avoid division by zero.
// Values below this are silently clamped.
const MinTarget = gas.Gas(1)

// MaxTarget is the maximum allowable [Time.Target] to avoid overflows of the
// associated [proxytime.Time.Rate]. Values above this are silently clamped.
const MaxTarget = gas.Gas(math.MaxUint64 / TargetToRate)

func rateOf(target gas.Gas) gas.Gas { return target * TargetToRate }
func clampTarget(t gas.Gas) gas.Gas { return min(max(t, MinTarget), MaxTarget) }

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
	return &Time{
		Time:   tm.Time.Clone(),
		target: tm.target,
		excess: tm.excess,
		config: tm.config,
	}
}

// Target returns the `T` parameter of ACP-176.
func (tm *Time) Target() gas.Gas {
	return tm.target
}

// Excess returns the `x` variable of ACP-176.
func (tm *Time) Excess() gas.Gas {
	return tm.excess
}

// Price returns the price of a unit of gas, i.e. the "base fee", determined by
// [gas.CalculatePrice]. However, when [hook.GasPriceConfig.StaticPricing] is
// true, Price always returns [hook.GasPriceConfig.MinPrice].
func (tm *Time) Price() gas.Price {
	if tm.config.staticPricing {
		return tm.config.minPrice
	}
	// TODO (ceyonur): Consider omitting `MinPrice` in favor of `MinExcess`.
	// https://github.com/ava-labs/avalanchego/vms/saevm/issues/267
	return gas.CalculatePrice(tm.config.minPrice, tm.excess, tm.excessScalingFactor())
}

// excessScalingFactor returns the K variable of ACP-103/176, i.e.
// [config.targetToExcessScaling] * T, capped at [math.MaxUint64].
func (tm *Time) excessScalingFactor() gas.Gas {
	return intmath.BoundedMultiply(tm.config.targetToExcessScaling, tm.target, math.MaxUint64)
}

// BaseFee is equivalent to [Time.Price], returning the result as a uint256 for
// compatibility with geth/libevm objects.
func (tm *Time) BaseFee() *uint256.Int {
	return uint256.NewInt(uint64(tm.Price()))
}

// SetRate is equivalent to [Time.SetTarget] after (integer) division of `r` by
// [TargetToRate].
func (tm *Time) SetRate(r gas.Gas) {
	tm.SetTarget(r / TargetToRate)
}

// SetTarget changes the target gas consumption per second, clamping the
// argument to the range [[MinTarget], [MaxTarget]]. If the [Time.Excess] were
// to overflow as a result of this scaling then it is silently capped at
// [math.MaxUint64].
func (tm *Time) SetTarget(t gas.Gas) {
	t = clampTarget(t)
	r := rateOf(t)

	x, err := tm.Scale(tm.excess, r)
	if err != nil {
		x = math.MaxUint64
	}

	tm.Time.SetRate(r)
	tm.excess = x
	tm.target = t
}

// Tick is equivalent to [proxytime.Time.Tick] except that it also updates the
// gas excess.
func (tm *Time) Tick(g gas.Gas) {
	tm.Time.Tick(g)

	R, T := tm.Rate(), tm.Target()
	quo, _, _ := intmath.MulDiv(g, R-T, R) // overflow is impossible as (R-T)/R < 1
	tm.excess = intmath.BoundedAdd(tm.excess, quo, math.MaxUint64)
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
