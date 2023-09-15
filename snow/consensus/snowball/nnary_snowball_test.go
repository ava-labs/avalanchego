// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNnarySnowball(t *testing.T) {
	require := require.New(t)

	betaVirtuous := 2
	betaRogue := 2

	sb := nnarySnowball{}
	sb.Initialize(betaVirtuous, betaRogue, Red)
	sb.Add(Blue)
	sb.Add(Green)

	require.Equal(Red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(Blue)
	require.Equal(Blue, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(Red)
	require.Equal(Blue, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(Blue)
	require.Equal(Blue, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(Blue)
	require.Equal(Blue, sb.Preference())
	require.True(sb.Finalized())
}

func TestVirtuousNnarySnowball(t *testing.T) {
	require := require.New(t)

	betaVirtuous := 1
	betaRogue := 2

	sb := nnarySnowball{}
	sb.Initialize(betaVirtuous, betaRogue, Red)

	require.Equal(Red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(Red)
	require.Equal(Red, sb.Preference())
	require.True(sb.Finalized())
}

func TestNarySnowballRecordUnsuccessfulPoll(t *testing.T) {
	require := require.New(t)

	betaVirtuous := 2
	betaRogue := 2

	sb := nnarySnowball{}
	sb.Initialize(betaVirtuous, betaRogue, Red)
	sb.Add(Blue)

	require.Equal(Red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(Blue)
	require.Equal(Blue, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordUnsuccessfulPoll()

	sb.RecordSuccessfulPoll(Blue)

	require.Equal(Blue, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(Blue)

	require.Equal(Blue, sb.Preference())
	require.True(sb.Finalized())

	expected := "SB(Preference = TtF4d2QWbk5vzQGTEPrN48x6vwgAoAmKQ9cbp79inpQmcRKES, NumSuccessfulPolls = 3, SF(Confidence = 2, Finalized = true, SL(Preference = TtF4d2QWbk5vzQGTEPrN48x6vwgAoAmKQ9cbp79inpQmcRKES)))"
	require.Equal(expected, sb.String())

	for i := 0; i < 4; i++ {
		sb.RecordSuccessfulPoll(Red)

		require.Equal(Blue, sb.Preference())
		require.True(sb.Finalized())
	}
}

func TestNarySnowballDifferentSnowflakeColor(t *testing.T) {
	require := require.New(t)

	betaVirtuous := 2
	betaRogue := 2

	sb := nnarySnowball{}
	sb.Initialize(betaVirtuous, betaRogue, Red)
	sb.Add(Blue)

	require.Equal(Red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordSuccessfulPoll(Blue)

	require.Equal(Blue, sb.nnarySnowflake.Preference())

	sb.RecordSuccessfulPoll(Red)

	require.Equal(Blue, sb.Preference())
	require.Equal(Red, sb.nnarySnowflake.Preference())
}
