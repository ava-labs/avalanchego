// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import "fmt"

var _ Unary = (*unarySnowball)(nil)

func newUnarySnowball(beta int) unarySnowball {
	return unarySnowball{
		unarySnowflake: newUnarySnowflake(beta),
	}
}

// unarySnowball is the implementation of a unary snowball instance
type unarySnowball struct {
	// wrap the unary snowflake logic
	unarySnowflake

	// preferenceStrength tracks the total number of polls with a preference
	preferenceStrength int
}

func (sb *unarySnowball) RecordSuccessfulPoll() {
	sb.preferenceStrength++
	sb.unarySnowflake.RecordSuccessfulPoll()
}

func (sb *unarySnowball) RecordPollPreference() {
	sb.preferenceStrength++
	sb.unarySnowflake.RecordUnsuccessfulPoll()
}

func (sb *unarySnowball) Extend(beta int, choice int) Binary {
	bs := &binarySnowball{
		binarySnowflake: binarySnowflake{
			binarySlush: binarySlush{preference: choice},
			confidence:  sb.confidence,
			beta:        beta,
			finalized:   sb.Finalized(),
		},
		preference: choice,
	}
	bs.preferenceStrength[choice] = sb.preferenceStrength
	return bs
}

func (sb *unarySnowball) Clone() Unary {
	newSnowball := *sb
	return &newSnowball
}

func (sb *unarySnowball) String() string {
	return fmt.Sprintf("SB(PreferenceStrength = %d, %s)",
		sb.preferenceStrength,
		&sb.unarySnowflake)
}
