// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

type snowball struct {
	// numSuccessfulPolls is the number of times this choice was the successful
	// result of a network poll
	numSuccessfulPolls int

	// confidence is the number of consecutive times this choice was the
	// successful result of a network poll as of [lastVote]
	confidence int

	// lastVote is the last poll number that this choice was included in a
	// successful network poll
	lastVote uint64

	// rogue identifies if there is a known conflict with this choice
	rogue bool
}

func (sb *snowball) Confidence(currentVote uint64) int {
	if sb.lastVote != currentVote {
		return 0
	}
	return sb.confidence
}

func (sb *snowball) RecordSuccessfulPoll(currentVote uint64) {
	// If this choice wasn't voted for during the last poll, the confidence
	// should have been reset during the last poll. So, we reset it now.
	if sb.lastVote+1 != currentVote {
		sb.confidence = 0
	}

	// This choice was voted for in this poll. Mark it as such.
	sb.lastVote = currentVote

	// An affirmative vote increases both the snowball and snowflake counters.
	sb.numSuccessfulPolls++
	sb.confidence++
}

func (sb *snowball) Finalized(betaVirtuous, betaRogue int) bool {
	// This choice is finalized if the snowflake counter is at least
	// [betaRogue]. If there are no known conflicts with this operation, it can
	// be accepted with a snowflake counter of at least [betaVirtuous].
	return (!sb.rogue && sb.confidence >= betaVirtuous) ||
		sb.confidence >= betaRogue
}
