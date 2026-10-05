// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
)

func TestForkHeightPersists(t *testing.T) {
	db := memdb.New()
	s := newTestState(t, db)

	_, ok := s.GetForkHeight()
	require.False(t, ok, "GetForkHeight() before set")

	s.SetForkHeight(77)
	require.NoError(t, s.Commit(), "Commit()")
	require.NoError(t, s.Close(), "Close()")

	reloaded := newTestState(t, db)
	height, ok := reloaded.GetForkHeight()
	require.True(t, ok, "GetForkHeight() after reload")
	require.Equal(t, uint64(77), height, "GetForkHeight() after reload")
}
