// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"
	"time"
)

func ValidateProposedChainTime(
	proposedChainTime,
	currentChainTime,
	nextStakerChangeTime,
	localTime time.Time,
) error {
	if !proposedChainTime.After(currentChainTime) {
		return fmt.Errorf(
			"proposed timestamp (%s), not after current timestamp (%s)",
			proposedChainTime,
			currentChainTime,
		)
	}

	// Only allow timestamp to move forward as far as the time of next staker
	// set change time
	if proposedChainTime.After(nextStakerChangeTime) {
		return fmt.Errorf(
			"proposed timestamp (%s) later than next staker change time (%s)",
			proposedChainTime,
			nextStakerChangeTime,
		)
	}

	// Note: this means we can only have sprees of <SyncBound> blocks
	// because we increment 1 sec each block and I cannot violate SyncBound
	localTimestampPlusSync := localTime.Add(SyncBound)
	if localTimestampPlusSync.Before(proposedChainTime) {
		return fmt.Errorf(
			"proposed time (%s) is too far in the future relative to local time (%s)",
			proposedChainTime,
			localTime,
		)
	}

	return nil
}
