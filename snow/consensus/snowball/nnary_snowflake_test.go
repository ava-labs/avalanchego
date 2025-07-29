// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestNnarySnowflake(t *testing.T) {
	require := require.New(t)

	alphaPreference, alphaConfidence := 1, 2
	beta := 2
	terminationConditions := newSingleTerminationCondition(alphaConfidence, beta)

	sf := newNnarySnowflake(alphaPreference, terminationConditions, Red)
	sf.Add(Blue)
	sf.Add(Green)

	require.Equal(Red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaConfidence, Blue)
	require.Equal(Blue, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaPreference, Red)
	require.Equal(Red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaConfidence, Red)
	require.Equal(Red, sf.Preference())
	require.False(sf.Finalized())

	sf.RecordPoll(alphaConfidence, Red)
	require.Equal(Red, sf.Preference())
	require.True(sf.Finalized())

	sf.RecordPoll(alphaPreference, Blue)
	require.Equal(Red, sf.Preference())
	require.True(sf.Finalized())

	sf.RecordPoll(alphaConfidence, Blue)
	require.Equal(Red, sf.Preference())
	require.True(sf.Finalized())
}

func TestNnarySnowflakeConfidenceReset(t *testing.T) {
	require := require.New(t)

	alphaPreference, alphaConfidence := 1, 2
	beta := 4
	terminationConditions := newSingleTerminationCondition(alphaConfidence, beta)

	sf := newNnarySnowflake(alphaPreference, terminationConditions, Red)
	sf.Add(Blue)
	sf.Add(Green)

	require.Equal(Red, sf.Preference())
	require.False(sf.Finalized())

	// Increase Blue's confidence without finalizing
	for i := 0; i < beta-1; i++ {
		sf.RecordPoll(alphaConfidence, Blue)
		require.Equal(Blue, sf.Preference())
		require.False(sf.Finalized())
	}

	// Increase Red's confidence without finalizing
	for i := 0; i < beta-1; i++ {
		sf.RecordPoll(alphaConfidence, Red)
		require.Equal(Red, sf.Preference())
		require.False(sf.Finalized())
	}

	// One more round of voting for Red should accept Red
	sf.RecordPoll(alphaConfidence, Red)
	require.Equal(Red, sf.Preference())
	require.True(sf.Finalized())
}

func TestVirtuousNnarySnowflake(t *testing.T) {
	require := require.New(t)

	alphaPreference, alphaConfidence := 1, 2
	beta := 2
	terminationConditions := newSingleTerminationCondition(alphaConfidence, beta)

	sb := newNnarySnowflake(alphaPreference, terminationConditions, Red)
	require.Equal(Red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordPoll(alphaConfidence, Red)
	require.Equal(Red, sb.Preference())
	require.False(sb.Finalized())

	sb.RecordPoll(alphaConfidence, Red)
	require.Equal(Red, sb.Preference())
	require.True(sb.Finalized())
}

type nnarySnowflakeTest struct {
	require *require.Assertions

	nnarySnowflake
}

func newNnarySnowflakeTest(t *testing.T, alphaPreference int, terminationConditions []terminationCondition) snowflakeTest[ids.ID] {
	require := require.New(t)

	return &nnarySnowflakeTest{
		require:        require,
		nnarySnowflake: newNnarySnowflake(alphaPreference, terminationConditions, Red),
	}
}

func (sf *nnarySnowflakeTest) RecordPoll(count int, choice ids.ID) {
	sf.nnarySnowflake.RecordPoll(count, choice)
}

func (sf *nnarySnowflakeTest) AssertEqual(expectedConfidences []int, expectedFinalized bool, expectedPreference ids.ID) {
	sf.require.Equal(expectedPreference, sf.Preference())
	sf.require.Equal(expectedConfidences, sf.nnarySnowflake.confidence)
	sf.require.Equal(expectedFinalized, sf.Finalized())
}

func TestNnarySnowflakeErrorDrivenSingleChoice(t *testing.T) {
	for _, test := range getErrorDrivenSnowflakeSingleChoiceSuite[ids.ID]() {
		t.Run(test.name, func(t *testing.T) {
			test.f(t, newNnarySnowflakeTest, Red)
		})
	}
}

func TestNnarySnowflakeErrorDrivenMultiChoice(t *testing.T) {
	for _, test := range getErrorDrivenSnowflakeMultiChoiceSuite[ids.ID]() {
		t.Run(test.name, func(t *testing.T) {
			test.f(t, newNnarySnowflakeTest, Red, Green)
		})
	}
}
