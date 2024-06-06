// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func UnarySnowflakeStateTest(t *testing.T, sf *unarySnowflake, expectedConfidences []int, expectedFinalized bool) {
	require := require.New(t)

	require.Equal(expectedConfidences, sf.confidence)
	require.Equal(expectedFinalized, sf.Finalized())
}

func TestUnarySnowflake(t *testing.T) {
	require := require.New(t)

	alphaPreference, alphaConfidence := 1, 2
	beta := 2
	terminationConditions := []terminationCondition{
		{
			alphaConfidence: alphaConfidence,
			beta:            beta,
		},
	}

	sf := newUnarySnowflake(alphaPreference, terminationConditions)

	sf.RecordPoll(alphaConfidence)
	UnarySnowflakeStateTest(t, &sf, []int{1}, false)

	sf.RecordUnsuccessfulPoll()
	UnarySnowflakeStateTest(t, &sf, []int{0}, false)

	sf.RecordPoll(alphaConfidence)
	UnarySnowflakeStateTest(t, &sf, []int{1}, false)

	sfCloneIntf := sf.Clone()
	require.IsType(&unarySnowflake{}, sfCloneIntf)
	sfClone := sfCloneIntf.(*unarySnowflake)

	UnarySnowflakeStateTest(t, sfClone, []int{1}, false)

	binarySnowflake := sfClone.Extend(0)

	binarySnowflake.RecordUnsuccessfulPoll()

	binarySnowflake.RecordPoll(alphaConfidence, 1)

	require.False(binarySnowflake.Finalized())

	binarySnowflake.RecordPoll(alphaConfidence, 1)

	require.Equal(1, binarySnowflake.Preference())
	require.True(binarySnowflake.Finalized())

	sf.RecordPoll(alphaConfidence)
	UnarySnowflakeStateTest(t, &sf, []int{2}, true)

	sf.RecordUnsuccessfulPoll()
	UnarySnowflakeStateTest(t, &sf, []int{0}, true)

	sf.RecordPoll(alphaConfidence)
	UnarySnowflakeStateTest(t, &sf, []int{1}, true)
}

func TestUnarySnowflakeErrorDriven(t *testing.T) {
	alphaPreference := 3
	terminationConditions := []terminationCondition{
		{
			alphaConfidence: 3,
			beta:            3,
		},
		{
			alphaConfidence: 4,
			beta:            2,
		},
		{
			alphaConfidence: 5,
			beta:            1,
		},
	}

	for i, terminationCondition := range terminationConditions {
		sf := newUnarySnowflake(alphaPreference, terminationConditions)

		for poll := 1; poll <= terminationCondition.beta; poll++ {
			sf.RecordPoll(terminationCondition.alphaConfidence)

			expectedConfidences := make([]int, len(terminationConditions))
			for j := 0; j < i+1; j++ {
				expectedConfidences[j] = poll
			}
			UnarySnowflakeStateTest(t, &sf, expectedConfidences, poll >= terminationCondition.beta)
		}
	}
}

func TestUnarySnowflakeErrorDrivenReset(t *testing.T) {
	alphaPreference := 3
	terminationConditions := []terminationCondition{
		{
			alphaConfidence: 3,
			beta:            4,
		},
		{
			alphaConfidence: 4,
			beta:            3,
		},
		{
			alphaConfidence: 5,
			beta:            2,
		},
	}

	for i, terminationCondition := range terminationConditions {
		sf := newUnarySnowflake(alphaPreference, terminationConditions)

		// Accumulate confidence up to 1 less than beta, reset, and confirm
		// expected behavior from fresh state.
		for poll := 0; poll < terminationCondition.beta-1; poll++ {
			sf.RecordPoll(terminationCondition.alphaConfidence)
		}
		sf.RecordUnsuccessfulPoll()
		zeroConfidence := make([]int, len(terminationConditions))
		UnarySnowflakeStateTest(t, &sf, zeroConfidence, false)

		for poll := 0; poll < terminationCondition.beta; poll++ {
			sf.RecordPoll(terminationCondition.alphaConfidence)

			expectedConfidences := make([]int, len(terminationConditions))
			for j := 0; j < i+1; j++ {
				expectedConfidences[j] = poll + 1
			}
			UnarySnowflakeStateTest(t, &sf, expectedConfidences, poll+1 >= terminationCondition.beta)
		}
	}
}

func TestUnarySnowflakeErrorDrivenResetHighestAlphaConfidence(t *testing.T) {
	alphaPreference := 3
	terminationConditions := []terminationCondition{
		{
			alphaConfidence: 3,
			beta:            4,
		},
		{
			alphaConfidence: 4,
			beta:            3,
		},
		{
			alphaConfidence: 5,
			beta:            2,
		},
	}

	sf := newUnarySnowflake(alphaPreference, terminationConditions)

	sf.RecordPoll(5)
	UnarySnowflakeStateTest(t, &sf, []int{1, 1, 1}, false)
	sf.RecordPoll(4)
	UnarySnowflakeStateTest(t, &sf, []int{2, 2, 0}, false)
	sf.RecordPoll(3)
	UnarySnowflakeStateTest(t, &sf, []int{3, 0, 0}, false)
	sf.RecordPoll(5)
	UnarySnowflakeStateTest(t, &sf, []int{4, 0, 0}, true)
}
