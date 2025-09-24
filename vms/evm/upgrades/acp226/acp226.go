// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// ACP-226 implements the dynamic minimum block delay mechanism specified here:
// https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/226-dynamic-minimum-block-times/README.md
package acp226

import "github.com/ava-labs/avalanchego/vms/evm/upgrades/common"

const (
	// MinDelayMilliseconds (M) is the minimum block delay in milliseconds
	MinDelayMilliseconds = 1 // ms
	// ConversionRate (D) is the conversion factor for exponential calculations
	ConversionRate = 1 << 20
	// MaxDelayExcessDiff (Q) is the maximum change in excess per update
	MaxDelayExcessDiff = 200

	maxDelayExcess = 46_516_320 // ConversionRate * ln(MaxUint64 / MinDelayMilliseconds) + 1
)

// acp226Params is the params used for the acp226 upgrade.
var acp226Params = common.ExcessParams{
	MinValue:       MinDelayMilliseconds,
	ConversionRate: ConversionRate,
	MaxExcessDiff:  MaxDelayExcessDiff,
	MaxExcess:      maxDelayExcess,
}

// DelayExcess represents the excess for delay calculation in the dynamic minimum block delay mechanism.
type DelayExcess uint64

// Delay returns the minimum block delay in milliseconds, `m`.
//
// Delay = MinDelayMilliseconds * e^(DelayExcess / ConversionRate)
func (t DelayExcess) Delay() uint64 {
	return acp226Params.CalculateValue(uint64(t))
}

// UpdateDelayExcess updates the DelayExcess to be as close as possible to the
// desiredDelayExcess without exceeding the maximum DelayExcess change.
func (t *DelayExcess) UpdateDelayExcess(desiredDelayExcess uint64) {
	*t = DelayExcess(acp226Params.AdjustExcess(uint64(*t), desiredDelayExcess))
}

// DesiredDelayExcess calculates the optimal delay excess given the desired
// delay.
func DesiredDelayExcess(desiredDelay uint64) uint64 {
	return acp226Params.DesiredExcess(desiredDelay)
}
