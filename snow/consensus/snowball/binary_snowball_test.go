// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBinarySnowball(t *testing.T) {
	require := require.New(t)

	red := 0
	blue := 1

	alphaPreference, alphaConfidence := 2, 3
	beta := 2
	terminationConditions := newSingleTerminationCondition(alphaConfidence, beta)

	sf := newBinarySnowflake(alphaPreference, terminationConditions, red)

	require.Equal(red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaConfidence, blue)
	require.Equal(blue, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaConfidence, red)
	require.Equal(red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaConfidence, blue)
	require.Equal(blue, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaConfidence, blue)
	require.Equal(blue, sf.Preference())
	require.True(sf.Finalized())
}

func TestBinarySnowballRecordPollPreference(t *testing.T) {
	require := require.New(t)

	red := 0
	blue := 1

	alphaPreference, alphaConfidence := 1, 2
	beta := 2
	terminationConditions := newSingleTerminationCondition(alphaConfidence, beta)

	sf := newBinarySnowflake(alphaPreference, terminationConditions, red)
	require.Equal(red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaConfidence, blue)
	require.Equal(blue, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaConfidence, red)
	require.Equal(red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaPreference, red)
	require.Equal(red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaConfidence, red)
	require.Equal(red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaConfidence, red)
	require.Equal(red, sf.Preference())
	require.True(sf.Finalized())

	expected := "SF(Confidence = [2], Finalized = true, SL(Preference = 0))"
	require.Equal(expected, sf.String())
}

func TestBinarySnowballRecordUnsuccessfulPoll(t *testing.T) {
	require := require.New(t)

	red := 0
	blue := 1

	alphaPreference, alphaConfidence := 1, 2
	beta := 2
	terminationConditions := newSingleTerminationCondition(alphaConfidence, beta)

	sf := newBinarySnowflake(alphaPreference, terminationConditions, red)
	require.Equal(red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaConfidence, blue)
	require.Equal(blue, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordUnsuccessfulPoll()

	sf.RecordPoll(alphaConfidence, blue)
	require.Equal(blue, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaConfidence, blue)
	require.Equal(blue, sf.Preference())
	require.True(sf.Finalized())

	expected := "SF(Confidence = [2], Finalized = true, SL(Preference = 1))"
	require.Equal(expected, sf.String())
}

func TestBinarySnowballAcceptWeirdColor(t *testing.T) {
	require := require.New(t)

	blue := 0
	red := 1

	alphaPreference, alphaConfidence := 1, 2
	beta := 2
	terminationConditions := newSingleTerminationCondition(alphaConfidence, beta)

	sf := newBinarySnowflake(alphaPreference, terminationConditions, red)

	require.Equal(red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaConfidence, red)
	sf.RecordUnsuccessfulPoll()

	require.Equal(red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaConfidence, red)

	sf.RecordUnsuccessfulPoll()

	require.Equal(red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaConfidence, blue)

	require.Equal(blue, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaConfidence, blue)

	require.Equal(blue, sf.Preference())
	require.True(sf.Finalized())

	expected := "SF(Confidence = [2], Finalized = true, SL(Preference = 0))"
	require.Equal(expected, sf.String())
}

func TestBinarySnowballLockColor(t *testing.T) {
	require := require.New(t)

	red := 0
	blue := 1

	alphaPreference, alphaConfidence := 1, 2
	beta := 1
	terminationConditions := newSingleTerminationCondition(alphaConfidence, beta)

	sb := newBinarySnowball(alphaPreference, terminationConditions, red)

	sb.RecordPoll(alphaConfidence, red)

	require.Equal(red, sb.Preference())
	require.True(sb.Finalized())

	sb.RecordPoll(alphaConfidence, blue)

	require.Equal(red, sb.Preference())
	require.True(sb.Finalized())

	sb.RecordPoll(alphaPreference, blue)
	sb.RecordPoll(alphaConfidence, blue)

	require.Equal(red, sb.Preference())
	require.True(sb.Finalized())

	expected := "SB(Preference = 1, PreferenceStrength[0] = 1, PreferenceStrength[1] = 3, SF(Confidence = [1], Finalized = true, SL(Preference = 0)))"
	require.Equal(expected, sb.String())
}
