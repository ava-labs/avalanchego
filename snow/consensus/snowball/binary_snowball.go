// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"fmt"
)

var _ BinarySnowball = &binarySnowball{}

// binarySnowball is the implementation of a binary snowball instance
type binarySnowball struct {
	// wrap the binary snowflake logic
	binarySnowflake

	// preference is the choice with the largest number of successful polls.
	// Ties are broken by switching choice lazily
	preference int

	// numSuccessfulPolls tracks the total number of successful network polls of
	// the 0 and 1 choices
	numSuccessfulPolls [2]int
}

func (sb *binarySnowball) Initialize(beta, choice int) {
	sb.binarySnowflake.Initialize(beta, choice)
	sb.preference = choice
}

func (sb *binarySnowball) Preference() int {
	// It is possible, with low probability, that the snowflake preference is
	// not equal to the snowball preference when snowflake finalizes. However,
	// this case is handled for completion. Therefore, if snowflake is
	// finalized, then our finalized snowflake choice should be preferred.
	if sb.Finalized() {
		return sb.binarySnowflake.Preference()
	}
	return sb.preference
}

func (sb *binarySnowball) RecordSuccessfulPoll(choice int) {
	sb.numSuccessfulPolls[choice]++
	if sb.numSuccessfulPolls[choice] > sb.numSuccessfulPolls[1-choice] {
		sb.preference = choice
	}
	sb.binarySnowflake.RecordSuccessfulPoll(choice)
}

func (sb *binarySnowball) String() string {
	return fmt.Sprintf(
		"SB(Preference = %d, NumSuccessfulPolls[0] = %d, NumSuccessfulPolls[1] = %d, %s)",
		sb.preference,
		sb.numSuccessfulPolls[0],
		sb.numSuccessfulPolls[1],
		&sb.binarySnowflake)
}
