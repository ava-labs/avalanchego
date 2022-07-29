// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrChildBlockEarlierThanParent     = errors.New("proposed timestamp not after current chain time")
	ErrChildBlockAfterStakerChangeTime = errors.New("proposed timestamp later than next staker change time")
	ErrChildBlockBeyondSyncBound       = errors.New("proposed timestamp is too far in the future relative to local time")
)

// proposedChainTime returns nil if the [proposedChainTime]
// is a valid chain time given the [currentChainTime], wall clock
// time [now] and when the staking set changes next ([nextStakerChangeTime]).
// The [proposedChainTime] must be >= currentChainTime and <= to
// [nextStakerChangeTime], so that no staking set changes are skipped.
// If [mustIncTimestamp], [proposedChainTime] must > [currentChainTime].
// The [proposedChainTime] must be within syncBound with respect
// to local clock, to make sure chain time approximates "real" time.
// In the example below, proposedChainTime is valid because
// it's after [currentChainTime] and before [nextStakerChangeTime]
// and [now] + [syncBound].
//
// -----|--------|---------X------------|----------------------|
//      ^        ^         ^            ^                      ^
//      |        |         |            |                      |
//  now |        |         |   now + syncBound                 |
//        currentChainTime |                          nextStakerChangeTime
//                  proposedChainTime
func ValidateProposedChainTime(
	proposedChainTime,
	currentChainTime,
	nextStakerChangeTime,
	now time.Time,
	mustIncTimestamp bool,
) error {
	if mustIncTimestamp {
		if !proposedChainTime.After(currentChainTime) {
			return fmt.Errorf(
				"%w, proposed timestamp (%s), chain time (%s)",
				ErrChildBlockEarlierThanParent,
				proposedChainTime,
				currentChainTime,
			)
		}
	} else {
		if proposedChainTime.Before(currentChainTime) {
			return fmt.Errorf(
				"%w, proposed timestamp (%s), chain time (%s)",
				ErrChildBlockEarlierThanParent,
				proposedChainTime,
				currentChainTime,
			)
		}
	}

	// Only allow timestamp to move forward as far as the time of next staker
	// set change time
	if proposedChainTime.After(nextStakerChangeTime) {
		return fmt.Errorf(
			"%w, proposed timestamp (%s), next staker change time (%s)",
			ErrChildBlockAfterStakerChangeTime,
			proposedChainTime,
			nextStakerChangeTime,
		)
	}

	// Note: this means we can only have sprees of <SyncBound> blocks
	// because we increment 1 sec each block and I cannot violate SyncBound
	upperBound := now.Add(SyncBound)
	if upperBound.Before(proposedChainTime) {
		return fmt.Errorf(
			"%w, proposed time (%s), local time (%s)",
			ErrChildBlockBeyondSyncBound,
			proposedChainTime,
			now,
		)
	}
	return nil
}
