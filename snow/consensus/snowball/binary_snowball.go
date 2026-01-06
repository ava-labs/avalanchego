// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import "fmt"

var _ Binary = (*binarySnowball)(nil)

func newBinarySnowball(alphaPreference int, terminationConditions []terminationCondition, choice int) binarySnowball {
	return binarySnowball{
		binarySnowflake: newBinarySnowflake(alphaPreference, terminationConditions, choice),
		preference:      choice,
	}
}

// binarySnowball is the implementation of a binary snowball instance
type binarySnowball struct {
	// wrap the binary snowflake logic
	binarySnowflake

	// preference is the choice with the largest number of polls which preferred
	// the color. Ties are broken by switching choice lazily
	preference int

	// preferenceStrength tracks the total number of network polls which
	// preferred each choice
	preferenceStrength [2]int
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

func (sb *binarySnowball) RecordPoll(count, choice int) {
	if count >= sb.alphaPreference {
		sb.preferenceStrength[choice]++
		if sb.preferenceStrength[choice] > sb.preferenceStrength[1-choice] {
			sb.preference = choice
		}
	}
	sb.binarySnowflake.RecordPoll(count, choice)
}

func (sb *binarySnowball) String() string {
	return fmt.Sprintf(
		"SB(Preference = %d, PreferenceStrength[0] = %d, PreferenceStrength[1] = %d, %s)",
		sb.preference,
		sb.preferenceStrength[0],
		sb.preferenceStrength[1],
		&sb.binarySnowflake)
}
