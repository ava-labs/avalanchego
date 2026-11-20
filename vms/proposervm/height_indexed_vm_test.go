// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/vms/proposervm/state"

	statelessblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

func TestPruneOldBlocksSkipsHeightIndexGaps(t *testing.T) {
	const (
		numHistoricalBlocks = 2
		lastAcceptedHeight  = 15
	)
	tests := []struct {
		name           string
		indexedHeights []uint64
		wantHeights    []uint64
	}{
		{
			// Blocks [1, 3] were accepted before state syncing to height 10.
			// Pruning used to delete them and then fail at height 4.
			name:           "accepted history below state sync summary",
			indexedHeights: []uint64{1, 2, 3, 10, 11, 12, 13, 14, 15},
			wantHeights:    []uint64{13, 14, 15},
		},
		{
			name:           "multiple gaps",
			indexedHeights: []uint64{1, 2, 5, 6, 10, 11, 12, 13, 14, 15},
			wantHeights:    []uint64{13, 14, 15},
		},
		{
			name:           "nothing indexed after gap",
			indexedHeights: []uint64{1, 2, 3},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			baseDB := memdb.New()
			vm := &VM{
				Config: Config{
					NumHistoricalBlocks: numHistoricalBlocks,
				},
				ctx:                snowtest.Context(t, snowtest.CChainID),
				db:                 versiondb.New(baseDB),
				lastAcceptedHeight: lastAcceptedHeight,
			}
			vm.State = state.New(vm.db)

			blkIDs := make(map[uint64]ids.ID)
			for _, height := range test.indexedHeights {
				blk, err := statelessblock.BuildUnsigned(
					ids.GenerateTestID(),
					time.Time{},
					0,
					statelessblock.Epoch{},
					nil,
				)
				require.NoError(err)
				require.NoError(vm.State.PutBlock(blk))
				require.NoError(vm.State.SetBlockIDAtHeight(height, blk.ID()))
				blkIDs[height] = blk.ID()
			}
			require.NoError(vm.db.Commit())

			require.NoError(vm.pruneOldBlocks())

			// A fresh state over baseDB only sees committed pruning.
			committed := state.New(versiondb.New(baseDB))
			for height, blkID := range blkIDs {
				gotID, err := committed.GetBlockIDAtHeight(height)
				_, getBlockErr := committed.GetBlock(blkID)
				if slices.Contains(test.wantHeights, height) {
					require.NoError(err, "height %d", height)
					require.Equal(blkID, gotID, "height %d", height)
					require.NoError(getBlockErr, "height %d", height)
				} else {
					require.ErrorIs(err, database.ErrNotFound, "height %d", height)
					require.ErrorIs(getBlockErr, database.ErrNotFound, "height %d", height)
				}
			}
		})
	}
}
