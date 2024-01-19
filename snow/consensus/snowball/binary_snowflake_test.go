// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBinarySnowflake(t *testing.T) {
	require := require.New(t)

	blue := 0
	red := 1

	beta := 2

	sf := newBinarySnowflake(beta, red)

	require.Equal(red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordSuccessfulPoll(blue)

	require.Equal(blue, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordSuccessfulPoll(red)

	require.Equal(red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordSuccessfulPoll(blue)

	require.Equal(blue, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPollPreference(red)
	require.Equal(red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordSuccessfulPoll(blue)
	require.Equal(blue, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordSuccessfulPoll(blue)
	require.Equal(blue, sf.Preference())
	require.True(sf.Finalized())
}
