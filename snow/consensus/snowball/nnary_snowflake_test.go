// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNnarySnowflake(t *testing.T) {
	require := require.New(t)

	betaVirtuous := 2
	betaRogue := 2

	sf := newNnarySnowflake(betaVirtuous, betaRogue, Red)
	sf.Add(Blue)
	sf.Add(Green)

	require.Equal(Red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordSuccessfulPoll(Blue)
	require.Equal(Blue, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPollPreference(Red)
	require.Equal(Red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordSuccessfulPoll(Red)
	require.Equal(Red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordSuccessfulPoll(Red)
	require.Equal(Red, sf.Preference())
	require.True(sf.Finalized())

	sf.RecordPollPreference(Blue)
	require.Equal(Red, sf.Preference())
	require.True(sf.Finalized())

	sf.RecordSuccessfulPoll(Blue)
	require.Equal(Red, sf.Preference())
	require.True(sf.Finalized())
}

func TestNnarySnowflakeConfidenceReset(t *testing.T) {
	require := require.New(t)

	betaVirtuous := 4
	betaRogue := 4

	sf := newNnarySnowflake(betaVirtuous, betaRogue, Red)
	sf.Add(Blue)
	sf.Add(Green)

	require.Equal(Red, sf.Preference())
	require.False(sf.Finalized())

	// Increase Blue's confidence without finalizing
	for i := 0; i < betaRogue-1; i++ {
		sf.RecordSuccessfulPoll(Blue)
		require.Equal(Blue, sf.Preference())
		require.False(sf.Finalized())
	}

	// Increase Red's confidence without finalizing
	for i := 0; i < betaRogue-1; i++ {
		sf.RecordSuccessfulPoll(Red)
		require.Equal(Red, sf.Preference())
		require.False(sf.Finalized())
	}

	// One more round of voting for Red should accept Red
	sf.RecordSuccessfulPoll(Red)
	require.Equal(Red, sf.Preference())
	require.True(sf.Finalized())
}

func TestVirtuousNnarySnowflake(t *testing.T) {
	require := require.New(t)

	betaVirtuous := 2
	betaRogue := 3

	sb := newNnarySnowflake(betaVirtuous, betaRogue, Red)
	require.Equal(Red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(Red)
	require.Equal(Red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(Red)
	require.Equal(Red, sb.Preference())
	require.True(sb.Finalized())
}

func TestRogueNnarySnowflake(t *testing.T) {
	require := require.New(t)

	betaVirtuous := 1
	betaRogue := 2

	sb := newNnarySnowflake(betaVirtuous, betaRogue, Red)
	require.False(sb.rogue)

	sb.Add(Red)
	require.False(sb.rogue)

	sb.Add(Blue)
	require.True(sb.rogue)

	sb.Add(Red)
	require.True(sb.rogue)

	require.Equal(Red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(Red)
	require.Equal(Red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(Red)
	require.Equal(Red, sb.Preference())
	require.True(sb.Finalized())
}
