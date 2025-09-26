// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
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
)

// GetNextTimestamp calculates the timestamp (in seconds and milliseconds) for the next child block based on the parent's timestamp and the current time.
// First return value is the timestamp in seconds, second return value is the timestamp in milliseconds.
func GetNextTimestamp(parent *types.Header, now time.Time) (uint64, uint64) {
	var (
		timestamp   = uint64(now.Unix())
		timestampMS = uint64(now.UnixMilli())
	)
	// Note: in order to support asynchronous block production, blocks are allowed to have
	// the same timestamp as their parent. This allows more than one block to be produced
	// per second.
	parentExtra := customtypes.GetHeaderExtra(parent)
	if parent.Time >= timestamp ||
		(parentExtra.TimeMilliseconds != nil && *parentExtra.TimeMilliseconds >= timestampMS) {
		timestamp = parent.Time
		// If the parent has a TimeMilliseconds, use it. Otherwise, use the parent time * 1000.
		if parentExtra.TimeMilliseconds != nil {
			timestampMS = *parentExtra.TimeMilliseconds
		} else {
			timestampMS = parent.Time * 1000 // TODO: establish minimum time
		}
	}
	return timestamp, timestampMS
}

// VerifyTime verifies that the header's Time and TimeMilliseconds fields are
// consistent with the given rules and the current time.
// This includes:
// - TimeMilliseconds is nil before Granite activation
// - TimeMilliseconds is non-nil after Granite activation
// - Time matches TimeMilliseconds/1000 after Granite activation
// - Time/TimeMilliseconds is not too far in the future
// - Time/TimeMilliseconds is non-decreasing
// - (TODO) Minimum block delay is enforced
func VerifyTime(extraConfig *extras.ChainConfig, parent *types.Header, header *types.Header, now time.Time) error {
	var (
		headerExtra = customtypes.GetHeaderExtra(header)
		parentExtra = customtypes.GetHeaderExtra(parent)
	)

	// Verify the header's timestamp is not earlier than parent's
	// it does include equality(==), so multiple blocks per second is ok
	if header.Time < parent.Time {
		return fmt.Errorf("%w: %d < parent %d", errBlockTooOld, header.Time, parent.Time)
	}

	// Do all checks that apply only before Granite
	if !extraConfig.IsGranite(header.Time) {
		// Make sure the block isn't too far in the future
		if maxBlockTime := uint64(now.Add(MaxFutureBlockTime).Unix()); header.Time > maxBlockTime {
			return fmt.Errorf("%w: %d > allowed %d", ErrBlockTooFarInFuture, header.Time, maxBlockTime)
		}

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

	// Verify TimeMilliseconds is not earlier than parent's TimeMilliseconds
	// TODO: Ensure minimum block delay is enforced
	if parentExtra.TimeMilliseconds != nil && *headerExtra.TimeMilliseconds < *parentExtra.TimeMilliseconds {
		return fmt.Errorf("%w: %d < parent %d",
			errBlockTooOld,
			*headerExtra.TimeMilliseconds,
			*parentExtra.TimeMilliseconds,
		)
	}

	// Verify TimeMilliseconds is not too far in the future
	if maxBlockTimeMillis := uint64(now.Add(MaxFutureBlockTime).UnixMilli()); *headerExtra.TimeMilliseconds > maxBlockTimeMillis {
		return fmt.Errorf("%w: %d > allowed %d",
			ErrBlockTooFarInFuture,
			*headerExtra.TimeMilliseconds,
			maxBlockTimeMillis,
		)
	}

	return nil
}
