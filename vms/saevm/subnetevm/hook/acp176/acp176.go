// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package acp176 provides the [TargetExcess] gas-target vote carried in
// subnet-evm SAE header extras, as specified by
// https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/176-dynamic-evm-gas-limit-and-price-discovery-updates/README.md
//
// All math delegates to the shared [acp176] state machine; this package only
// contributes the header-friendly value type.
//
// TODO: unify with cchain's `dynamic.TargetExponent`, which is the same value
// under a different name, once the two chains' header-extra types can share
// an exponent package.
package acp176

import (
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/acp176"
)

// TargetToExcessScaling is the default ratio between the gas target and the
// reciprocal of the excess coefficient used in price calculation (K = 87 * T,
// 87 ~= 60 / ln(2)).
const TargetToExcessScaling = 87

// Re-exports of the shared ACP-176 parameters this VM's callers consume.
const (
	MinTargetPerSecond  = acp176.MinTargetPerSecond  // P
	MaxTargetExcessDiff = acp176.MaxTargetExcessDiff // Q
	MinPrice            = acp176.MinGasPrice         // M
)

// A TargetExcess determines the gas target via
// Target = MinTargetPerSecond * e^(TargetExcess / TargetConversion).
type TargetExcess uint64

// Target returns the target gas per second.
func (t TargetExcess) Target() gas.Gas {
	s := acp176.State{TargetExcess: gas.Gas(t)}
	return s.Target()
}

// UpdateTargetExcess updates the TargetExcess to be as close as possible to
// the desiredTargetExcess without changing by more than
// [acp176.MaxTargetExcessDiff].
func (t *TargetExcess) UpdateTargetExcess(desiredTargetExcess TargetExcess) {
	s := acp176.State{TargetExcess: gas.Gas(*t)}
	s.UpdateTargetExcess(gas.Gas(desiredTargetExcess))
	*t = TargetExcess(s.TargetExcess)
}

// DesiredTargetExcess calculates the optimal target excess given the desired
// target in gas.
func DesiredTargetExcess(desired gas.Gas) TargetExcess {
	return TargetExcess(acp176.DesiredTargetExcess(desired))
}
