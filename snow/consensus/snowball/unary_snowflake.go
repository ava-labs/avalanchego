// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import "fmt"

var _ Unary = (*unarySnowflake)(nil)

func newUnarySnowflake(beta int) unarySnowflake {
	return unarySnowflake{
		beta: beta,
	}
}

// unarySnowflake is the implementation of a unary snowflake instance
type unarySnowflake struct {
	// beta is the number of consecutive successful queries required for
	// finalization.
	beta int

	// confidence tracks the number of successful polls in a row that have
	// returned the preference
	confidence int

	// finalized prevents the state from changing after the required number of
	// consecutive polls has been reached
	finalized bool
}

func (sf *unarySnowflake) RecordSuccessfulPoll() {
	sf.confidence++
	sf.finalized = sf.finalized || sf.confidence >= sf.beta
}

// RecordPollPreference fails to reach an alpha threshold to increase our
// confidence, so this calls RecordUnsuccessfulPoll to reset the confidence
// counter.
func (sf *unarySnowflake) RecordPollPreference() {
	sf.RecordUnsuccessfulPoll()
}

func (sf *unarySnowflake) RecordUnsuccessfulPoll() {
	sf.confidence = 0
}

func (sf *unarySnowflake) Finalized() bool {
	return sf.finalized
}

func (sf *unarySnowflake) Extend(beta int, choice int) Binary {
	return &binarySnowflake{
		binarySlush: binarySlush{preference: choice},
		confidence:  sf.confidence,
		beta:        beta,
		finalized:   sf.finalized,
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
