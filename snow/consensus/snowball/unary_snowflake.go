// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"fmt"
)

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

// Initialize implements the UnarySnowflake interface
func (sf *unarySnowflake) Initialize(beta int) { sf.beta = beta }

// RecordSuccessfulPoll implements the UnarySnowflake interface
func (sf *unarySnowflake) RecordSuccessfulPoll() {
	sf.confidence++
	sf.finalized = sf.finalized || sf.confidence >= sf.beta
}

// RecordUnsuccessfulPoll implements the UnarySnowflake interface
func (sf *unarySnowflake) RecordUnsuccessfulPoll() { sf.confidence = 0 }

// Finalized implements the UnarySnowflake interface
func (sf *unarySnowflake) Finalized() bool { return sf.finalized }

// Extend implements the UnarySnowflake interface
func (sf *unarySnowflake) Extend(beta int, choice int) BinarySnowflake {
	return &binarySnowflake{
		binarySlush: binarySlush{preference: choice},
		confidence:  sf.confidence,
		beta:        beta,
		finalized:   sf.finalized,
	}
}

// Clone implements the UnarySnowflake interface
func (sf *unarySnowflake) Clone() UnarySnowflake {
	newSnowflake := *sf
	return &newSnowflake
}

func (sf *unarySnowflake) String() string {
	return fmt.Sprintf("SF(Confidence = %d, Finalized = %v)",
		sf.confidence,
		sf.finalized)
}
