// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

func TestResponder(t *testing.T) {
	// The chain is longer than maxBlocksPerResponse so the cap truncates a full
	// walk before genesis.
	chain := synctest.MakeChain(t, maxBlocksPerResponse+10)
	db := synctest.NewBlockDB(chain)
	tip := chain[len(chain)-1].NumberU64()

	// tipFirst[i] encodes the block i hops below the tip.
	tipFirst := encodeTipFirst(t, chain, len(chain))

	tests := []struct {
		name       string
		height     uint64
		numParents uint32
		cancelCtx  bool
		want       [][]byte
		wantErr    *common.AppError
	}{
		{
			name:       "returns_requested_parents_tip_first",
			height:     tip,
			numParents: 5,
			want:       tipFirst[:6], // the block at height plus 5 parents
		},
		{
			name:       "zero_parents_serves_the_block_alone",
			height:     tip,
			numParents: 0,
			want:       tipFirst[:1],
		},
		{
			name:       "includes_genesis_then_stops",
			height:     5,
			numParents: 100, // more than the blocks below the height
			want:       encodeTipFirst(t, chain[:6], 6),
		},
		{
			name:       "caps_parents_at_max",
			height:     tip,
			numParents: maxParentsPerRequest + 50,
			want:       tipFirst[:maxBlocksPerResponse],
		},
		{
			name:       "missing_block_rejected",
			height:     tip + 1,
			numParents: 1,
			wantErr:    errBlockNotFound,
		},
		{
			// [GetAncestors] serves the requested block even past its
			// deadline, so a cancelled walk still returns it.
			name:       "cancelled_context_still_serves_the_requested_block",
			height:     tip,
			numParents: 10,
			cancelCtx:  true,
			want:       tipFirst[:1],
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &responder{
				log: loggingtest.New(t, logging.Debug),
				db:  db,
			}

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.cancelCtx {
				cancel()
			}

			resp, appErr := r.Respond(ctx, ids.GenerateTestNodeID(), &syncpb.GetBlockRequest{
				Height:     tt.height,
				NumParents: tt.numParents,
			})
			if tt.wantErr != nil {
				require.ErrorIs(t, appErr, tt.wantErr)
				require.Nil(t, resp, "a rejected request carries no response")
				return
			}
			require.Nil(t, appErr) // A typed-nil error is non-nil so [require.NoError] would fail here.
			require.NotNil(t, resp)
			require.Equal(t, tt.want, resp.Blocks)
		})
	}
}

func TestErrorSentinels(t *testing.T) {
	synctest.RequireDistinctAppErrors(t, map[string]*common.AppError{
		"errBlockNotFound": errBlockNotFound,
	})
}
