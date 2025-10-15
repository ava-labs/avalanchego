// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/subnet-evm/params/extras"
	"github.com/ava-labs/subnet-evm/plugin/evm/customtypes"
)

var (
	// Max time from current time allowed for blocks, before they're considered future blocks
	// and fail verification
	MaxFutureBlockTime = 10 * time.Second

	errBlockTooOld                   = errors.New("block timestamp is too old")
	ErrBlockTooFarInFuture           = errors.New("block timestamp is too far in the future")
	ErrTimeMillisecondsRequired      = errors.New("TimeMilliseconds is required after Granite activation")
	ErrTimeMillisecondsMismatched    = errors.New("TimeMilliseconds does not match header.Time")
	ErrTimeMillisecondsBeforeGranite = errors.New("TimeMilliseconds should be nil before Granite activation")
	ErrMinDelayNotMet                = errors.New("minimum block delay not met")
	ErrGraniteClockBehindParent      = errors.New("current timestamp is not allowed to be behind than parent timestamp in Granite")
)

// GetNextTimestamp calculates the time for the next header based on the parent's timestamp and the current time.
// This can return the parent time if now is before the parent time and TimeMilliseconds is not set (pre-Granite).
func GetNextTimestamp(parent *types.Header, now time.Time) time.Time {
	parentExtra := customtypes.GetHeaderExtra(parent)
	// In Granite, there is a minimum delay enforced, so we cannot adjust the time with the parent's timestamp.
	// Instead we should have waited enough time before calling this function and before the block building.
	// We return the current time instead regardless and defer the verification to VerifyTime.
	if parent.Time < uint64(now.Unix()) || parentExtra.TimeMilliseconds != nil {
		return now
	}

	// In pre-Granite, blocks are allowed to have the same timestamp as their parent.
	return time.Unix(int64(parent.Time), 0)
}

// VerifyTime verifies that the header's Time and TimeMilliseconds fields are
// consistent with the given rules and the current time.
// This includes:
// - TimeMilliseconds is nil before Granite activation
// - TimeMilliseconds is non-nil after Granite activation
// - Time matches TimeMilliseconds/1000 after Granite activation
// - Time/TimeMilliseconds is not too far in the future
// - Time/TimeMilliseconds is non-decreasing
// - Minimum block delay is enforced
func VerifyTime(extraConfig *extras.ChainConfig, parent *types.Header, header *types.Header, now time.Time) error {
	var (
		headerExtra = customtypes.GetHeaderExtra(header)
		parentExtra = customtypes.GetHeaderExtra(parent)
	)

	// These two variables are backward-compatible with Time (seconds) fields.
	headerTimeMS := customtypes.HeaderTimeMilliseconds(header)
	parentTimeMS := customtypes.HeaderTimeMilliseconds(parent)

	// Verify the header's timestamp is not earlier than parent's
	// This includes equality(==), so multiple blocks per milliseconds is ok
	// pre-Granite.
	if headerTimeMS < parentTimeMS {
		return fmt.Errorf("%w: %d < parent %d", errBlockTooOld, headerTimeMS, parentTimeMS)
	}

	// Verify if the header's timestamp is not too far in the future
	if maxBlockTimeMS := uint64(now.Add(MaxFutureBlockTime).UnixMilli()); headerTimeMS > maxBlockTimeMS {
		return fmt.Errorf("%w: %d > allowed %d",
			ErrBlockTooFarInFuture,
			headerTimeMS,
			maxBlockTimeMS,
		)
	}

	if !extraConfig.IsGranite(header.Time) {
		// This field should not be set yet.
		if headerExtra.TimeMilliseconds != nil {
			return ErrTimeMillisecondsBeforeGranite
		}
		return nil
	}

	// After Granite, TimeMilliseconds is required
	if headerExtra.TimeMilliseconds == nil {
		return ErrTimeMillisecondsRequired
	}

	// Validate TimeMilliseconds is consistent with header.Time
	if expectedTime := *headerExtra.TimeMilliseconds / 1000; header.Time != expectedTime {
		return fmt.Errorf("%w: header.Time (%d) != TimeMilliseconds/1000 = (%d)",
			ErrTimeMillisecondsMismatched,
			header.Time,
			expectedTime,
		)
	}

	// Verify minimum block delay is enforced
	// Parent might not have a min delay excess if this is the first Granite block
	// in this case we cannot verify the min delay,
	// Otherwise parent should have been verified in VerifyMinDelayExcess
	if parentExtra.MinDelayExcess == nil {
		return nil
	}

	// This should not be underflow as we have verified that the parent's
	// TimeMilliseconds is earlier than the header's TimeMilliseconds above.
	actualDelayMS := headerTimeMS - parentTimeMS
	minRequiredDelayMS := parentExtra.MinDelayExcess.Delay()
	if actualDelayMS < minRequiredDelayMS {
		return fmt.Errorf("%w: actual delay %dms < required %dms",
			ErrMinDelayNotMet,
			actualDelayMS,
			minRequiredDelayMS,
		)
	}

	return nil
}
