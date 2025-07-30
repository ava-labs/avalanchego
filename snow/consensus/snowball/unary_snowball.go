// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"fmt"
	"slices"
)

var _ Unary = (*unarySnowball)(nil)

func newUnarySnowball(alphaPreference int, terminationConditions []terminationCondition) unarySnowball {
	return unarySnowball{
		unarySnowflake: newUnarySnowflake(alphaPreference, terminationConditions),
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
			binarySlush:           binarySlush{preference: choice},
			confidence:            slices.Clone(sb.confidence),
			alphaPreference:       sb.alphaPreference,
			terminationConditions: sb.terminationConditions,
			finalized:             sb.Finalized(),
		},
		preference: choice,
	}
	bs.preferenceStrength[choice] = sb.preferenceStrength
	return bs
}

func (sb *unarySnowball) Clone() Unary {
	newSnowball := *sb
	newSnowball.confidence = slices.Clone(sb.confidence)
	return &newSnowball
}

func (sb *unarySnowball) String() string {
	return fmt.Sprintf("SB(PreferenceStrength = %d, %s)",
		sb.preferenceStrength,
		&sb.unarySnowflake)
}
