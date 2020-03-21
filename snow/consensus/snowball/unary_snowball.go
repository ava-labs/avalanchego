// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"fmt"
)

// unarySnowball is the implementation of a unary snowball instance
type unarySnowball struct {
	// beta is the number of consecutive successful queries required for
	// finalization.
	beta int

	// confidence tracks the number of successful polls in a row that have
	// returned the preference
	confidence int

	// numSuccessfulPolls tracks the total number of successful network polls
	numSuccessfulPolls int

	// finalized prevents the state from changing after the required number of
	// consecutive polls has been reached
	finalized bool
}

// Initialize implements the UnarySnowball interface
func (sb *unarySnowball) Initialize(beta int) { sb.beta = beta }

// RecordSuccessfulPoll implements the UnarySnowball interface
func (sb *unarySnowball) RecordSuccessfulPoll() {
	sb.numSuccessfulPolls++
	sb.confidence++
	sb.finalized = sb.finalized || sb.confidence >= sb.beta
}

// RecordUnsuccessfulPoll implements the UnarySnowball interface
func (sb *unarySnowball) RecordUnsuccessfulPoll() { sb.confidence = 0 }

// Finalized implements the UnarySnowball interface
func (sb *unarySnowball) Finalized() bool { return sb.finalized }

// Extend implements the UnarySnowball interface
func (sb *unarySnowball) Extend(beta int, choice int) BinarySnowball {
	bs := &binarySnowball{
		binarySnowflake: binarySnowflake{
			beta:       beta,
			preference: choice,
			finalized:  sb.Finalized(),
		},
		preference: choice,
	}
	return bs
}

// Clone implements the UnarySnowball interface
func (sb *unarySnowball) Clone() UnarySnowball {
	return &unarySnowball{
		beta:               sb.beta,
		numSuccessfulPolls: sb.numSuccessfulPolls,
		confidence:         sb.confidence,
		finalized:          sb.Finalized(),
	}
}

func (sb *unarySnowball) String() string {
	return fmt.Sprintf("SB(NumSuccessfulPolls = %d, Confidence = %d, Finalized = %v)",
		sb.numSuccessfulPolls,
		sb.confidence,
		sb.Finalized())
}
