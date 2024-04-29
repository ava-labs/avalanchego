// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import "fmt"

var _ Unary = (*unarySnowflake)(nil)

func newUnarySnowflake(alphaPreference, alphaConfidence, beta int) unarySnowflake {
	return unarySnowflake{
		alphaPreference: alphaPreference,
		alphaConfidence: alphaConfidence,
		beta:            beta,
	}
}

// unarySnowflake is the implementation of a unary snowflake instance
type unarySnowflake struct {
	// beta is the number of consecutive successful queries required for
	// finalization.
	beta int

	// alphaPreference is the threshold required to update the preference
	alphaPreference int

	// alphaConfidence is the threshold required to increment the confidence counter
	alphaConfidence int

	// confidence tracks the number of successful polls in a row that have
	// returned the preference
	confidence int

	// finalized prevents the state from changing after the required number of
	// consecutive polls has been reached
	finalized bool
}

func (sf *unarySnowflake) RecordPoll(count int) {
	if count < sf.alphaConfidence {
		sf.RecordUnsuccessfulPoll()
		return
	}

	sf.confidence++
	sf.finalized = sf.finalized || sf.confidence >= sf.beta
}

func (sf *unarySnowflake) RecordUnsuccessfulPoll() {
	sf.confidence = 0
}

func (sf *unarySnowflake) Finalized() bool {
	return sf.finalized
}

func (sf *unarySnowflake) Extend(choice int) Binary {
	return &binarySnowflake{
		binarySlush:     binarySlush{preference: choice},
		confidence:      sf.confidence,
		alphaPreference: sf.alphaPreference,
		alphaConfidence: sf.alphaConfidence,
		beta:            sf.beta,
		finalized:       sf.finalized,
	}
}

func (sf *unarySnowflake) Clone() Unary {
	newSnowflake := *sf
	return &newSnowflake
}

func (sf *unarySnowflake) String() string {
	return fmt.Sprintf("SF(Confidence = %d, Finalized = %v)",
		sf.confidence,
		sf.finalized)
}
