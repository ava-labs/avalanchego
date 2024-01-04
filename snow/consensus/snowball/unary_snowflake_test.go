// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func UnarySnowflakeStateTest(t *testing.T, sf *unarySnowflake, expectedConfidence int, expectedFinalized bool) {
	require := require.New(t)

	require.Equal(expectedConfidence, sf.confidence)
	require.Equal(expectedFinalized, sf.Finalized())
}

func TestUnarySnowflake(t *testing.T) {
	require := require.New(t)

	beta := 2

	sf := newUnarySnowflake(beta)

	sf.RecordSuccessfulPoll()
	UnarySnowflakeStateTest(t, &sf, 1, false)

	sf.RecordUnsuccessfulPoll()
	UnarySnowflakeStateTest(t, &sf, 0, false)

	sf.RecordSuccessfulPoll()
	UnarySnowflakeStateTest(t, &sf, 1, false)

	sfCloneIntf := sf.Clone()
	require.IsType(&unarySnowflake{}, sfCloneIntf)
	sfClone := sfCloneIntf.(*unarySnowflake)

	UnarySnowflakeStateTest(t, sfClone, 1, false)

	binarySnowflake := sfClone.Extend(beta, 0)

	binarySnowflake.RecordUnsuccessfulPoll()

	binarySnowflake.RecordSuccessfulPoll(1)

	require.False(binarySnowflake.Finalized())

	binarySnowflake.RecordSuccessfulPoll(1)

	require.Equal(1, binarySnowflake.Preference())
	require.True(binarySnowflake.Finalized())

	sf.RecordSuccessfulPoll()
	UnarySnowflakeStateTest(t, &sf, 2, true)

	sf.RecordUnsuccessfulPoll()
	UnarySnowflakeStateTest(t, &sf, 0, true)

	sf.RecordSuccessfulPoll()
	UnarySnowflakeStateTest(t, &sf, 1, true)
}
