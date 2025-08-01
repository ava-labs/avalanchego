// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import "fmt"

func newBinarySlush(choice int) binarySlush {
	return binarySlush{
		preference: choice,
	}
}

// binarySlush is the implementation of a binary slush instance
type binarySlush struct {
	// preference is the choice that last had a successful poll. Unless there
	// hasn't been a successful poll, in which case it is the initially provided
	// choice.
	preference int
}

func (sl *binarySlush) Preference() int {
	return sl.preference
}

func (sl *binarySlush) RecordSuccessfulPoll(choice int) {
	sl.preference = choice
}

func (sl *binarySlush) String() string {
	return fmt.Sprintf("SL(Preference = %d)", sl.preference)
}
