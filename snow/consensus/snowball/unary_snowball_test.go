// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func UnarySnowballStateTest(t *testing.T, sb *unarySnowball, expectedPreferenceStrength int, expectedConfidence []int, expectedFinalized bool) {
	require := require.New(t)

	require.Equal(expectedPreferenceStrength, sb.preferenceStrength)
	require.Equal(expectedConfidence, sb.confidence)
	require.Equal(expectedFinalized, sb.Finalized())
}

func TestUnarySnowball(t *testing.T) {
	require := require.New(t)

	alphaPreference, alphaConfidence := 1, 2
	beta := 2
	terminationConditions := newSingleTerminationCondition(alphaConfidence, beta)

	sb := newUnarySnowball(alphaPreference, terminationConditions)

	sb.RecordPoll(alphaConfidence)
	UnarySnowballStateTest(t, &sb, 1, []int{1}, false)

	sb.RecordPoll(alphaPreference)
	UnarySnowballStateTest(t, &sb, 2, []int{0}, false)

	sb.RecordPoll(alphaConfidence)
	UnarySnowballStateTest(t, &sb, 3, []int{1}, false)

	sb.RecordUnsuccessfulPoll()
	UnarySnowballStateTest(t, &sb, 3, []int{0}, false)

	sb.RecordPoll(alphaConfidence)
	UnarySnowballStateTest(t, &sb, 4, []int{1}, false)

	sbCloneIntf := sb.Clone()
	require.IsType(&unarySnowball{}, sbCloneIntf)
	sbClone := sbCloneIntf.(*unarySnowball)

	UnarySnowballStateTest(t, sbClone, 4, []int{1}, false)

	binarySnowball := sbClone.Extend(0)

	expected := "SB(Preference = 0, PreferenceStrength[0] = 4, PreferenceStrength[1] = 0, SF(Confidence = [1], Finalized = false, SL(Preference = 0)))"
	require.Equal(expected, binarySnowball.String())

	binarySnowball.RecordUnsuccessfulPoll()
	for i := 0; i < 5; i++ {
		require.Zero(binarySnowball.Preference())
		require.False(binarySnowball.Finalized())
		binarySnowball.RecordPoll(alphaConfidence, 1)
		binarySnowball.RecordUnsuccessfulPoll()
	}

	require.Equal(1, binarySnowball.Preference())
	require.False(binarySnowball.Finalized())

	binarySnowball.RecordPoll(alphaConfidence, 1)
	require.Equal(1, binarySnowball.Preference())
	require.False(binarySnowball.Finalized())

	binarySnowball.RecordPoll(alphaConfidence, 1)
	require.Equal(1, binarySnowball.Preference())
	require.True(binarySnowball.Finalized())

	expected = "SB(PreferenceStrength = 4, SF(Confidence = [1], Finalized = false))"
	require.Equal(expected, sb.String())
}
