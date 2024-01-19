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

	beta := 2

	sb := newBinarySnowball(beta, red)
	require.Equal(red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(blue)
	require.Equal(blue, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(red)
	require.Equal(blue, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(blue)
	require.Equal(blue, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(blue)
	require.Equal(blue, sb.Preference())
	require.True(sb.Finalized())
}

func TestBinarySnowballRecordPollPreference(t *testing.T) {
	require := require.New(t)

	red := 0
	blue := 1

	beta := 2

	sb := newBinarySnowball(beta, red)
	require.Equal(red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(blue)
	require.Equal(blue, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(red)
	require.Equal(blue, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordPollPreference(red)
	require.Equal(red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(red)
	require.Equal(red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(red)
	require.Equal(red, sb.Preference())
	require.True(sb.Finalized())

	expected := "SB(Preference = 0, PreferenceStrength[0] = 4, PreferenceStrength[1] = 1, SF(Confidence = 2, Finalized = true, SL(Preference = 0)))"
	require.Equal(expected, sb.String())
}

func TestBinarySnowballRecordUnsuccessfulPoll(t *testing.T) {
	require := require.New(t)

	red := 0
	blue := 1

	beta := 2

	sb := newBinarySnowball(beta, red)
	require.Equal(red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(blue)
	require.Equal(blue, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordUnsuccessfulPoll()

	sb.RecordSuccessfulPoll(blue)
	require.Equal(blue, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(blue)
	require.Equal(blue, sb.Preference())
	require.True(sb.Finalized())

	expected := "SB(Preference = 1, PreferenceStrength[0] = 0, PreferenceStrength[1] = 3, SF(Confidence = 2, Finalized = true, SL(Preference = 1)))"
	require.Equal(expected, sb.String())
}

func TestBinarySnowballAcceptWeirdColor(t *testing.T) {
	require := require.New(t)

	blue := 0
	red := 1

	beta := 2

	sb := newBinarySnowball(beta, red)

	require.Equal(red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(red)
	sb.RecordUnsuccessfulPoll()

	require.Equal(red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(red)

	sb.RecordUnsuccessfulPoll()

	require.Equal(red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(blue)

	require.Equal(red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(blue)

	require.Equal(blue, sb.Preference())
	require.True(sb.Finalized())

	expected := "SB(Preference = 1, PreferenceStrength[0] = 2, PreferenceStrength[1] = 2, SF(Confidence = 2, Finalized = true, SL(Preference = 0)))"
	require.Equal(expected, sb.String())
}

func TestBinarySnowballLockColor(t *testing.T) {
	require := require.New(t)

	red := 0
	blue := 1

	beta := 1

	sb := newBinarySnowball(beta, red)

	sb.RecordSuccessfulPoll(red)

	require.Equal(red, sb.Preference())
	require.True(sb.Finalized())

	sb.RecordSuccessfulPoll(blue)

	require.Equal(red, sb.Preference())
	require.True(sb.Finalized())

	sb.RecordPollPreference(blue)
	sb.RecordSuccessfulPoll(blue)

	require.Equal(red, sb.Preference())
	require.True(sb.Finalized())

	expected := "SB(Preference = 1, PreferenceStrength[0] = 1, PreferenceStrength[1] = 3, SF(Confidence = 1, Finalized = true, SL(Preference = 0)))"
	require.Equal(expected, sb.String())
}
