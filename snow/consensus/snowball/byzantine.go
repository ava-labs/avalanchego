// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"github.com/ava-labs/gecko/ids"
)

// ByzantineFactory implements Factory by returning a byzantine struct
type ByzantineFactory struct{}

// New implements Factory
func (ByzantineFactory) New() Consensus { return &Byzantine{} }

// Byzantine is a naive implementation of a multi-choice snowball instance
type Byzantine struct {
	// params contains all the configurations of a snowball instance
	params Parameters

	// Hardcode the preference
	preference ids.ID
}

// Initialize implements the Consensus interface
func (b *Byzantine) Initialize(params Parameters, choice ids.ID) {
	b.params = params
	b.preference = choice
}

// Parameters implements the Consensus interface
func (b *Byzantine) Parameters() Parameters { return b.params }

// Add implements the Consensus interface
func (b *Byzantine) Add(choice ids.ID) {}

// Preference implements the Consensus interface
func (b *Byzantine) Preference() ids.ID { return b.preference }

// RecordPoll implements the Consensus interface
func (b *Byzantine) RecordPoll(votes ids.Bag) {}

// RecordUnsuccessfulPoll implements the Consensus interface
func (b *Byzantine) RecordUnsuccessfulPoll() {}

// Finalized implements the Consensus interface
func (b *Byzantine) Finalized() bool { return true }
func (b *Byzantine) String() string  { return b.preference.String() }
