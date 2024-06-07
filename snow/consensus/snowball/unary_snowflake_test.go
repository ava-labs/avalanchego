// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func UnarySnowflakeStateTest(t *testing.T, sf *unarySnowflake, expectedConfidence []int, expectedFinalized bool) {
	require := require.New(t)

	require.Equal(expectedConfidence, sf.confidence)
	require.Equal(expectedFinalized, sf.Finalized())
}

func TestUnarySnowflake(t *testing.T) {
	require := require.New(t)

	alphaPreference, alphaConfidence := 1, 2
	beta := 2

	sf := newUnarySnowflake(alphaPreference, alphaConfidence, beta)

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
