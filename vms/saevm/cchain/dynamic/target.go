// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamic

import "github.com/ava-labs/avalanchego/vms/components/gas"

// TargetExponent encodes the target gas per second.
//
// Implements ACP-176, specified here:
// https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/176-dynamic-evm-gas-limit-and-price-discovery-updates/README.md
type TargetExponent uint64

// Target returns the target gas per second.
//
// Target = minimum * e^(t / conversionRate)
func (t TargetExponent) Target() gas.Gas {
	const (
		minimum        = 1_000_000 // gas
		conversionRate = 1 << 25
	)
	return gas.Gas(gas.CalculatePrice(
		minimum,
		gas.Gas(t),
		conversionRate,
	))
}

// Toward returns a new value where t is moved to be as close as possible to
// desired without changing by more than allowed.
//
// If desired is nil, t is returned unmodified.
func (t TargetExponent) Toward(desired *TargetExponent) TargetExponent {
	// Per ACP-176, the per-block exponent change is capped at 2^15.
	const maxDiff = 1 << 15
	return toward(t, desired, maxDiff)
}

// DesiredTargetExponent calculates the optimal target exponent given the
// desired target in gas.
func DesiredTargetExponent(desired gas.Gas) TargetExponent {
	// Binary search avoids the rounding error of a floating-point solution.
	const maxExponent = 1_024_950_627 // conversionRate * ln(MaxUint64 / minimum) + 1
	return search(maxExponent, func(guess TargetExponent) bool {
		return guess.Target() >= desired
	})
}
