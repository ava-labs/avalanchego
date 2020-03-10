// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"fmt"

	"github.com/ava-labs/gecko/ids"
)

// nnarySnowflake is the implementation of a snowflake instance with an
// unbounded number of choices
type nnarySnowflake struct {
	// betaVirtuous is the number of consecutive successful queries required for
	// finalization on a virtuous instance.
	betaVirtuous int

	// betaRogue is the number of consecutive successful queries required for
	// finalization on a rogue instance.
	betaRogue int

	// confidence tracks the number of successful polls in a row that have
	// returned the preference
	confidence int

	// preference is the choice that last had a successful poll. Unless there
	// hasn't been a successful poll, in which case it is the initially provided
	// choice.
	preference ids.ID

	// rogue tracks if this instance has multiple choices or only one
	rogue bool

	// finalized prevents the state from changing after the required number of
	// consecutive polls has been reached
	finalized bool
}

// Initialize implements the NnarySnowflake interface
func (sf *nnarySnowflake) Initialize(betaVirtuous, betaRogue int, choice ids.ID) {
	sf.betaVirtuous = betaVirtuous
	sf.betaRogue = betaRogue
	sf.preference = choice
}

// Add implements the NnarySnowflake interface
func (sf *nnarySnowflake) Add(choice ids.ID) { sf.rogue = sf.rogue || !choice.Equals(sf.preference) }

// Preference implements the NnarySnowflake interface
func (sf *nnarySnowflake) Preference() ids.ID { return sf.preference }

// RecordSuccessfulPoll implements the NnarySnowflake interface
func (sf *nnarySnowflake) RecordSuccessfulPoll(choice ids.ID) {
	if sf.Finalized() {
		return
	}

	if sf.preference.Equals(choice) {
		sf.confidence++
	} else {
		sf.confidence = 1
		sf.preference = choice
	}

	sf.finalized = (!sf.rogue && sf.confidence >= sf.betaVirtuous) ||
		sf.confidence >= sf.betaRogue
}

// RecordUnsuccessfulPoll implements the NnarySnowflake interface
func (sf *nnarySnowflake) RecordUnsuccessfulPoll() { sf.confidence = 0 }

// Finalized implements the NnarySnowflake interface
func (sf *nnarySnowflake) Finalized() bool { return sf.finalized }

func (sf *nnarySnowflake) String() string {
	return fmt.Sprintf("SF(Preference = %s, Confidence = %d, Finalized = %v)",
		sf.preference,
		sf.confidence,
		sf.Finalized())
}
