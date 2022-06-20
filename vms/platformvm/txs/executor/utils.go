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

func ValidateProposedChainTime(
	proposedChainTime,
	currentChainTime,
	nextStakerChangeTime,
	localTime time.Time,
) error {
	if !proposedChainTime.After(currentChainTime) {
		return fmt.Errorf(
			"%w, proposed timestamp (%s), chain time (%s)",
			ErrChildBlockEarlierThanParent,
			proposedChainTime,
			currentChainTime,
		)
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
	localTimestampPlusSync := localTime.Add(SyncBound)
	if localTimestampPlusSync.Before(proposedChainTime) {
		return fmt.Errorf(
			"%w, proposed time (%s), local time (%s)",
			ErrChildBlockBeyondSyncBound,
			proposedChainTime,
			localTime,
		)
	}

	return nil
}
