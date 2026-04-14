// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"time"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/strevm/gastime"
	"github.com/ava-labs/strevm/hook"
	"github.com/ava-labs/strevm/proxytime"
)

// PreciseTime calls [hook.Points.SubSecondBlockTime] on the header and returns
// the value, combined with the regular timestamp to provide a full-resolution
// block time.
func PreciseTime(hooks hook.Points, hdr *types.Header) time.Time {
	return preciseTime(hdr, hooks.SubSecondBlockTime(hdr))
}

func preciseTime(hdr *types.Header, subSec time.Duration) time.Time { //nolint:staticcheck // subSec intentionally communicates that the value is < time.Second
	return time.Unix(
		int64(hdr.Time), //nolint:gosec // Won't overflow for a few millennia
		subSec.Nanoseconds(),
	)
}

// GasTime is the gas equivalent of [PreciseTime], deriving the gas rate from
// the parent header and the hooks.
func GasTime(hooks hook.Points, hdr, parent *types.Header) *proxytime.Time[gas.Gas] {
	target, _ := hooks.GasConfigAfter(parent)
	rate := gastime.SafeRateOfTarget(target)
	tm := proxytime.New(hdr.Time, rate)
	tm.Tick(gastime.SubSecond(hooks, hdr, rate))
	return tm
}
