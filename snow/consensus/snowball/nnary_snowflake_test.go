// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNnarySnowflake(t *testing.T) {
	require := require.New(t)

	beta := 2

	sf := newNnarySnowflake(beta, Red)
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

	beta := 4

	sf := newNnarySnowflake(beta, Red)
	sf.Add(Blue)
	sf.Add(Green)

	require.Equal(Red, sf.Preference())
	require.False(sf.Finalized())

	// Increase Blue's confidence without finalizing
	for i := 0; i < beta-1; i++ {
		sf.RecordSuccessfulPoll(Blue)
		require.Equal(Blue, sf.Preference())
		require.False(sf.Finalized())
	}

	// Increase Red's confidence without finalizing
	for i := 0; i < beta-1; i++ {
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

	beta := 2

	sb := newNnarySnowflake(beta, Red)
	require.Equal(Red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(Red)
	require.Equal(Red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(Red)
	require.Equal(Red, sb.Preference())
	require.True(sb.Finalized())
}
