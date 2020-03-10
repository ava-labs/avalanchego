// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"github.com/ava-labs/gecko/ids"
)

// FlatFactory implements Factory by returning a flat struct
type FlatFactory struct{}

// New implements Factory
func (FlatFactory) New() Consensus { return &Flat{} }

// Flat is a naive implementation of a multi-choice snowball instance
type Flat struct {
	// params contains all the configurations of a snowball instance
	params Parameters

	// snowball wraps the n-nary snowball logic
	snowball nnarySnowball
}

// Initialize implements the Consensus interface
func (f *Flat) Initialize(params Parameters, choice ids.ID) {
	f.params = params
	f.snowball.Initialize(params.BetaVirtuous, params.BetaRogue, choice)
}

// Parameters implements the Consensus interface
func (f *Flat) Parameters() Parameters { return f.params }

// Add implements the Consensus interface
func (f *Flat) Add(choice ids.ID) { f.snowball.Add(choice) }

// Preference implements the Consensus interface
func (f *Flat) Preference() ids.ID { return f.snowball.Preference() }

// RecordPoll implements the Consensus interface
func (f *Flat) RecordPoll(votes ids.Bag) {
	if pollMode, numVotes := votes.Mode(); numVotes >= f.params.Alpha {
		f.snowball.RecordSuccessfulPoll(pollMode)
	} else {
		f.RecordUnsuccessfulPoll()
	}
}

// RecordUnsuccessfulPoll implements the Consensus interface
func (f *Flat) RecordUnsuccessfulPoll() { f.snowball.RecordUnsuccessfulPoll() }

// Finalized implements the Consensus interface
func (f *Flat) Finalized() bool { return f.snowball.Finalized() }
func (f *Flat) String() string  { return f.snowball.String() }
