// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
)

func newIndexedState(t testing.TB, blks []block.Block) State {
	t.Helper()

	vdb := versiondb.New(memdb.New())
	st := New(vdb)
	for i, blk := range blks {
		require.NoError(t, st.PutBlock(blk))
		require.NoError(t, st.SetBlockIDAtHeight(uint64(i), blk.ID()))
	}
	require.NoError(t, st.SetForkHeight(0))
	require.NoError(t, vdb.Commit())
	return st
}

func TestGetBlockIDsInRange(t *testing.T) {
	require := require.New(t)

	blks := buildChain(t, 20, 64)
	st := newIndexedState(t, blks)

	got, err := st.GetBlockIDsInRange(5, 9)
	require.NoError(err)
	require.Len(got, 5)
	for i, blkID := range got {
		require.Equal(blks[5+i].ID(), blkID)
	}

	// A range running past the end of the index returns what exists.
	got, err = st.GetBlockIDsInRange(18, 25)
	require.NoError(err)
	require.Len(got, 2)

	// An inverted range is empty, not an error.
	got, err = st.GetBlockIDsInRange(9, 5)
	require.NoError(err)
	require.Empty(got)
}
