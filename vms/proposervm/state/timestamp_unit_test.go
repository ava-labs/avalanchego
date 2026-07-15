// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
)

func TestVerifyTimestampUnit(t *testing.T) {
	require := require.New(t)

	vdb := versiondb.New(memdb.New())

	// A fresh chain adopts the configured unit and holds it across restarts.
	require.NoError(New(vdb, true).VerifyTimestampUnit(true))
	require.NoError(New(vdb, true).VerifyTimestampUnit(true))

	// A restart with the unit flipped (e.g. a lost subnet config) must fail
	// loud instead of silently misparsing every stored block, in both
	// directions.
	err := New(vdb, false).VerifyTimestampUnit(false)
	require.ErrorIs(err, errTimestampUnitMismatch)

	vdb = versiondb.New(memdb.New())
	require.NoError(New(vdb, false).VerifyTimestampUnit(false))
	err = New(vdb, true).VerifyTimestampUnit(true)
	require.ErrorIs(err, errTimestampUnitMismatch)
}

func TestVerifyTimestampUnitExistingChain(t *testing.T) {
	require := require.New(t)

	// A database from before unit tracking (an upgraded node) with existing
	// blocks: enabling millisecond timestamps is rejected, seconds is adopted.
	vdb := versiondb.New(memdb.New())
	s := New(vdb, false)
	require.NoError(s.SetLastAccepted(ids.GenerateTestID()))

	err := s.VerifyTimestampUnit(true)
	require.ErrorIs(err, errMillisOnExistingChain)
	require.NoError(s.VerifyTimestampUnit(false))
}
