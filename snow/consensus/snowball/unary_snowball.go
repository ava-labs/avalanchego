// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import "fmt"

var _ Unary = (*unarySnowball)(nil)

func newUnarySnowball(alphaPreference, alphaConfidence, beta int) unarySnowball {
	return unarySnowball{
		unarySnowflake: newUnarySnowflake(alphaPreference, alphaConfidence, beta),
	}
}

// unarySnowball is the implementation of a unary snowball instance
type unarySnowball struct {
	// wrap the unary snowflake logic
	unarySnowflake

	// preferenceStrength tracks the total number of polls with a preference
	preferenceStrength int
}

func (sb *unarySnowball) RecordPoll(count int) {
	if count >= sb.alphaPreference {
		sb.preferenceStrength++
	}
	sb.unarySnowflake.RecordPoll(count)
}

func (sb *unarySnowball) Extend(choice int) Binary {
	bs := &binarySnowball{
		binarySnowflake: binarySnowflake{
			binarySlush:     binarySlush{preference: choice},
			confidence:      sb.confidence,
			alphaPreference: sb.alphaPreference,
			alphaConfidence: sb.alphaConfidence,
			beta:            sb.beta,
			finalized:       sb.Finalized(),
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
