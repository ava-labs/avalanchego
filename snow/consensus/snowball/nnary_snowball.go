// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

// nnarySnowball is a naive implementation of a multi-color snowball instance
type nnarySnowball struct {
	// wrap the n-nary snowflake logic
	nnarySnowflake

	// preference is the choice with the largest number of successful polls.
	// Ties are broken by switching choice lazily
	preference ids.ID

	// maxSuccessfulPolls maximum number of successful polls this instance has
	// gotten for any choice
	maxSuccessfulPolls int

	// numSuccessfulPolls tracks the total number of successful network polls of
	// the choices
	numSuccessfulPolls map[ids.ID]int
}

// Initialize implements the NnarySnowball interface
func (sb *nnarySnowball) Initialize(betaVirtuous, betaRogue int, choice ids.ID) {
	sb.nnarySnowflake.Initialize(betaVirtuous, betaRogue, choice)
	sb.preference = choice
	sb.numSuccessfulPolls = make(map[ids.ID]int)
}

// Preference implements the NnarySnowball interface
func (sb *nnarySnowball) Preference() ids.ID {
	// It is possible, with low probability, that the snowflake preference is
	// not equal to the snowball preference when snowflake finalizes. However,
	// this case is handled for completion. Therefore, if snowflake is
	// finalized, then our finalized snowflake choice should be preferred.
	if sb.Finalized() {
		return sb.nnarySnowflake.Preference()
	}
	return sb.preference
}

// RecordSuccessfulPoll implements the NnarySnowball interface
func (sb *nnarySnowball) RecordSuccessfulPoll(choice ids.ID) {
	numSuccessfulPolls := sb.numSuccessfulPolls[choice] + 1
	sb.numSuccessfulPolls[choice] = numSuccessfulPolls

	if numSuccessfulPolls > sb.maxSuccessfulPolls {
		sb.preference = choice
		sb.maxSuccessfulPolls = numSuccessfulPolls
	}

	sb.nnarySnowflake.RecordSuccessfulPoll(choice)
}

func (sb *nnarySnowball) String() string {
	return fmt.Sprintf("SB(Preference = %s, NumSuccessfulPolls = %d, %s)",
		sb.preference, sb.maxSuccessfulPolls, &sb.nnarySnowflake)
}
