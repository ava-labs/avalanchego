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

	ErrBlockTooFarInFuture           = errors.New("block timestamp is too far in the future")
	ErrTimeMillisecondsRequired      = errors.New("TimeMilliseconds is required after Granite activation")
	ErrTimeMillisecondsMismatched    = errors.New("TimeMilliseconds does not match header.Time")
	ErrTimeMillisecondsBeforeGranite = errors.New("TimeMilliseconds should be nil before Granite activation")
)

// VerifyTime verifies that the header's Time and TimeMilliseconds fields are
// consistent with the given rules and the current time.
// This includes:
// - Time is not too far in the future
// - TimeMilliseconds is nil before Granite activation
// - TimeMilliseconds is non-nil after Granite activation
// - Time matches TimeMilliseconds/1000 after Granite activation
// - (TODO) Time is strictly increasing if parent is also after Granite activation
// - (TODO) Minimum block delay is enforced
// - (TODO) More time-related checks from dummy.VerifyHeader
func VerifyTime(extraConfig *extras.ChainConfig, header *types.Header, now time.Time) error {
	headerExtra := customtypes.GetHeaderExtra(header)
	// Make sure the block isn't too far in the future
	if maxBlockTime := uint64(now.Add(MaxFutureBlockTime).Unix()); header.Time > maxBlockTime {
		return fmt.Errorf("%w: %d > allowed %d", ErrBlockTooFarInFuture, header.Time, maxBlockTime)
	}

	// Validate TimeMilliseconds field isn't present prior to Granite
	if !extraConfig.IsGranite(header.Time) {
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

	// TODO: If the parent is also after Granite, ensure time is strictly increasing
	return nil
}
