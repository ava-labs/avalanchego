// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func UnarySnowballStateTest(t *testing.T, sb *unarySnowball, expectedNumSuccessfulPolls, expectedConfidence int, expectedFinalized bool) {
	require := require.New(t)

	require.Equal(expectedNumSuccessfulPolls, sb.numSuccessfulPolls)
	require.Equal(expectedConfidence, sb.confidence)
	require.Equal(expectedFinalized, sb.Finalized())
}

func TestUnarySnowball(t *testing.T) {
	require := require.New(t)

	beta := 2

	sb := &unarySnowball{}
	sb.Initialize(beta)

	sb.RecordSuccessfulPoll()
	UnarySnowballStateTest(t, sb, 1, 1, false)

	sb.RecordUnsuccessfulPoll()
	UnarySnowballStateTest(t, sb, 1, 0, false)

	sb.RecordSuccessfulPoll()
	UnarySnowballStateTest(t, sb, 2, 1, false)

	sbCloneIntf := sb.Clone()
	require.IsType(&unarySnowball{}, sbCloneIntf)
	sbClone := sbCloneIntf.(*unarySnowball)

	UnarySnowballStateTest(t, sbClone, 2, 1, false)

	binarySnowball := sbClone.Extend(beta, 0)

	expected := "SB(Preference = 0, NumSuccessfulPolls[0] = 2, NumSuccessfulPolls[1] = 0, SF(Confidence = 1, Finalized = false, SL(Preference = 0)))"
	require.Equal(expected, binarySnowball.String())

	binarySnowball.RecordUnsuccessfulPoll()
	for i := 0; i < 3; i++ {
		require.Zero(binarySnowball.Preference())
		require.False(binarySnowball.Finalized())
		binarySnowball.RecordSuccessfulPoll(1)
		binarySnowball.RecordUnsuccessfulPoll()
	}

	require.Equal(1, binarySnowball.Preference())
	require.False(binarySnowball.Finalized())

	binarySnowball.RecordSuccessfulPoll(1)
	require.Equal(1, binarySnowball.Preference())
	require.False(binarySnowball.Finalized())

	binarySnowball.RecordSuccessfulPoll(1)
	require.Equal(1, binarySnowball.Preference())
	require.True(binarySnowball.Finalized())

	expected = "SB(NumSuccessfulPolls = 2, SF(Confidence = 1, Finalized = false))"
	require.Equal(expected, sb.String())
}
