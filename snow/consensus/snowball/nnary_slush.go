// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

func newNnarySlush(choice ids.ID) nnarySlush {
	return nnarySlush{
		preference: choice,
	}
}

// nnarySlush is the implementation of a slush instance with an unbounded number
// of choices
type nnarySlush struct {
	// preference is the choice that last had a successful poll. Unless there
	// hasn't been a successful poll, in which case it is the initially provided
	// choice.
	preference ids.ID
}

func (sl *nnarySlush) Preference() ids.ID {
	return sl.preference
}

func (sl *nnarySlush) RecordSuccessfulPoll(choice ids.ID) {
	sl.preference = choice
}

func (sl *nnarySlush) String() string {
	return fmt.Sprintf("SL(Preference = %s)", sl.preference)
}
