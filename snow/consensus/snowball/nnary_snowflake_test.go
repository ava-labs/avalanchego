// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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

	sf := nnarySnowflake{}
	sf.Initialize(betaVirtuous, betaRogue, Red)
	sf.Add(Blue)
	sf.Add(Green)

	require.Equal(Red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordSuccessfulPoll(Blue)
	require.Equal(Blue, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordSuccessfulPoll(Red)
	require.Equal(Red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordSuccessfulPoll(Red)
	require.Equal(Red, sf.Preference())
	require.True(sf.Finalized())

	sf.RecordSuccessfulPoll(Blue)
	require.Equal(Red, sf.Preference())
	require.True(sf.Finalized())
}

func TestVirtuousNnarySnowflake(t *testing.T) {
	require := require.New(t)

	betaVirtuous := 2
	betaRogue := 3

	sb := nnarySnowflake{}
	sb.Initialize(betaVirtuous, betaRogue, Red)
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

	sb := nnarySnowflake{}
	sb.Initialize(betaVirtuous, betaRogue, Red)
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
