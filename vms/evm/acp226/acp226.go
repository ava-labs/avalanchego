// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package acp226 preserves the legacy ACP-226 API.
//
// Deprecated: use package dynamic.
package acp226

import "github.com/ava-labs/avalanchego/vms/evm/dynamic"

const (
	MinDelayMilliseconds = 1
	ConversionRate       = 1 << 20
	MaxDelayExcessDiff   = 200

	InitialDelayExcess DelayExcess = DelayExcess(dynamic.InitialDelayExponent)
)

// DelayExcess is the legacy name for an ACP-226 delay exponent.
type DelayExcess uint64

// Delay returns the minimum block delay in milliseconds.
func (d DelayExcess) Delay() uint64 {
	return dynamic.DelayExponent(d).Delay()
}

// UpdateDelayExcess moves d toward desired by the ACP-226 per-block limit.
func (d *DelayExcess) UpdateDelayExcess(desired DelayExcess) {
	target := dynamic.DelayExponent(desired)
	*d = DelayExcess(dynamic.DelayExponent(*d).Toward(&target))
}

// DesiredDelayExcess returns the exponent for desired milliseconds.
func DesiredDelayExcess(desired uint64) DelayExcess {
	return DelayExcess(dynamic.DesiredDelayExponent(desired))
}
