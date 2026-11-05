// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package saeheader decodes the SAE fields that VMs built atop SAE store in
// their block headers.
package saeheader

import (
	"time"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/hook"
)

// Settled returns the [hook.Settled] stored in the provided header fields, or
// the zero value if any of them are nil.
func Settled(height, gasUnix, gasNumerator, excess *uint64) hook.Settled {
	if height == nil ||
		gasUnix == nil ||
		gasNumerator == nil ||
		excess == nil {
		return hook.Settled{}
	}
	return hook.Settled{
		Height:       *height,
		GasUnix:      *gasUnix,
		GasNumerator: gas.Gas(*gasNumerator),
		Excess:       gas.Gas(*excess),
	}
}

// Time returns the canonical wall-clock time of a block from its seconds and
// milliseconds header fields.
//
// The whole-second value is authoritative and the millisecond field only
// refines it below the second, so the two can never disagree on which second a
// block belongs to. This keeps a block's time stable even when a peer sends a
// header whose millisecond field is inconsistent with its seconds.
func Time(seconds, ms uint64) time.Time {
	subSecondNanos := int64(ms%1000) * int64(time.Millisecond)
	return time.Unix(int64(seconds), subSecondNanos) //#nosec G115 -- Won't overflow for a few millennia
}
