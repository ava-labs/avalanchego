// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
)

func TestState(t *testing.T) {
	a := require.New(t)

	db := memdb.New()
	vdb := versiondb.New(db)
	s, err := New(vdb)
	a.NoError(err)

	testBlockState(a, s)
	testChainState(a, s)
}

func TestMeteredState(t *testing.T) {
	a := require.New(t)

	db := memdb.New()
	vdb := versiondb.New(db)
	s, err := NewMetered(vdb, "", prometheus.NewRegistry())
	a.NoError(err)

	testBlockState(a, s)
	testChainState(a, s)
}

// We should delete any pending processing blocks on startup, in the event that
// a block which was previously processing in consensus never had accept/reject
// called on it (e.g. if went offline).
func TestPruneProcessingBlocksOnRestart(t *testing.T) {
	gBlk, err := block.BuildUnsigned(ids.Empty, time.Time{}, 0, []byte("genesis"))
	require.NoError(t, err)

	tests := []struct {
		name           string
		preferredBlks  []block.Block
		processingBlks []block.Block
	}{
		{
			//  [g]
			//   |
			// *[1]*
			//   |
			// *[2]*
			name: "single branch",
			preferredBlks: func() []block.Block {
				blk1, err := block.BuildUnsigned(gBlk.ID(), time.Time{}, 0, []byte("block 1"))
				require.NoError(t, err)

				blk2, err := block.BuildUnsigned(blk1.ID(), time.Time{}, 0, []byte("block 2"))
				require.NoError(t, err)

				return []block.Block{blk1, blk2}
			}(),
		},
		{
			//      [g]
			//    /    \
			// *[1]*   [3]
			//   |      |
			// *[2]*   [4]
			name: "multiple processing branches",
			preferredBlks: func() []block.Block {
				blk1, err := block.BuildUnsigned(gBlk.ID(), time.Time{}, 0, []byte("block 1"))
				require.NoError(t, err)

				blk2, err := block.BuildUnsigned(blk1.ID(), time.Time{}, 0, []byte("block 2"))
				require.NoError(t, err)

				return []block.Block{blk1, blk2}
			}(),
			processingBlks: func() []block.Block {
				blk3, err := block.BuildUnsigned(gBlk.ID(), time.Time{}, 0, []byte("block 3"))
				require.NoError(t, err)

				blk4, err := block.BuildUnsigned(blk3.ID(), time.Time{}, 0, []byte("block 4"))
				require.NoError(t, err)

				return []block.Block{blk3, blk4}
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			db := versiondb.New(memdb.New())
			state, err := New(db)
			require.NoError(err)
			require.NoError(state.PutBlock(gBlk, choices.Accepted))

			// Our preference is the tip of the preferred chain
			require.NoError(state.SetPreference(tt.preferredBlks[len(tt.preferredBlks)-1].ID()))

			for _, blk := range append(tt.preferredBlks, tt.processingBlks...) {
				require.NoError(state.PutProcessingBlock(blk.ID()))
				require.NoError(state.PutBlock(blk, choices.Processing))
			}

			// We should prune all blocks not in the preferred chain upon
			// restart
			require.NoError(db.Commit())
			state, err = New(db)
			require.NoError(err)

			for _, blk := range tt.processingBlks {
				ok, err := state.HasProcessingBlock(blk.ID())
				require.NoError(err)
				require.False(ok)

				_, _, err = state.GetBlock(blk.ID())
				require.ErrorIs(err, database.ErrNotFound)
			}

			for _, blk := range tt.preferredBlks {
				ok, err := state.HasProcessingBlock(blk.ID())
				require.NoError(err)
				require.True(ok)
			}
		})
	}
}
