// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package saeheader decodes SAE fields from block headers.
package saeheader

import "time"

// Time returns a block's time from its seconds and milliseconds header fields.
// Only the sub-second part of ms is used, so seconds always decides which
// second the block is in, even if ms disagrees.
func Time(seconds, ms uint64) time.Time {
	subSecondNanos := int64(ms%1000) * int64(time.Millisecond)
	return time.Unix(int64(seconds), subSecondNanos) //#nosec G115 -- Won't overflow for a few millennia
}
