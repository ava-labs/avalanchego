// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	avacommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

func TestResponder(t *testing.T) {
	// Every block in a chain encodes to the same size, so a budget can be
	// stated in whole blocks.
	blockBytes := len(encodeBlock(t, synctest.MakeChain(t, 1)[1]))

	tests := []struct {
		name       string
		chainLen   int
		numParents uint32
		noBlocks   bool
		cancelCtx  bool
		budget     int // zero leaves the default response budget
		wantBlocks int
		wantErr    *avacommon.AppError
	}{
		{
			name:       "returns_requested_parents_tip_first",
			chainLen:   10,
			numParents: 5,
			wantBlocks: 5,
		},
		{
			name:       "includes_genesis_then_stops",
			chainLen:   5,
			numParents: 100, // more than the chain length
			wantBlocks: 6,
		},
		{
			name:       "caps_parents_at_max",
			chainLen:   int(maxParentsPerRequest) + 10,
			numParents: uint32(maxParentsPerRequest) + 50,
			wantBlocks: int(maxParentsPerRequest),
		},
		{
			name:       "missing_block_rejected",
			noBlocks:   true,
			numParents: 1,
			wantErr:    errBlocksNotFound,
		},
		{
			// [GetAncestors] serves the requested block even past its
			// deadline, so a cancelled walk still returns it.
			name:       "cancelled_context_still_serves_the_requested_block",
			chainLen:   50,
			numParents: 10,
			cancelCtx:  true,
			wantBlocks: 1,
		},
		{
			name:       "cancelled_context_with_nothing_to_serve_rejected",
			noBlocks:   true,
			numParents: 10,
			cancelCtx:  true,
			wantErr:    errServingCancelled,
		},
		{
			name:       "zero_parents_rejected",
			chainLen:   10,
			numParents: 0,
			wantErr:    errNoParentsRequested,
		},
		{
			name:       "budget_fits_three_blocks",
			chainLen:   10,
			numParents: 5,
			budget:     3 * (blockBytes + wrappers.IntLen),
			wantBlocks: 3,
		},
		{
			name:       "oversized_block_served_alone",
			chainLen:   10,
			numParents: 5,
			budget:     blockBytes / 2,
			wantBlocks: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var blocks []*types.Block
			if !tt.noBlocks {
				blocks = synctest.MakeChain(t, tt.chainLen)
			}
			var opts []HandlerOption
			if tt.budget > 0 {
				opts = append(opts, WithMaxResponseBytes(tt.budget))
			}
			r := newResponder(loggingtest.New(t, logging.Debug), synctest.NewBlockDB(blocks), opts...)

			ctx := t.Context()
			if tt.cancelCtx {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel() // cancel before the responder runs
			}

			height := uint64(10) // no such block when the chain is empty
			if len(blocks) > 0 {
				height = blocks[len(blocks)-1].NumberU64()
			}

			resp, appErr := r.Respond(ctx, ids.GenerateTestNodeID(), &syncpb.GetBlockRequest{
				Height:     height,
				NumParents: tt.numParents,
			})
			if tt.wantErr != nil {
				require.ErrorIs(t, appErr, tt.wantErr)
				require.Nil(t, resp, "a rejected request carries no response")
				return
			}
			// A nil *AppError is not a nil error, so NoError would fail here.
			require.Nil(t, appErr)
			require.NotNil(t, resp)
			require.Len(t, resp.Blocks, tt.wantBlocks)

			// Blocks come back tip-first, walking parents.
			for i, raw := range resp.Blocks {
				var b types.Block
				require.NoError(t, rlp.DecodeBytes(raw, &b))
				want := blocks[len(blocks)-1-i]
				require.Equal(t, want.Hash(), b.Hash())
			}
		})
	}
}

func TestErrorSentinels(t *testing.T) {
	synctest.RequireDistinctAppErrors(t, map[string]*avacommon.AppError{
		"errBlocksNotFound":     errBlocksNotFound,
		"errNoParentsRequested": errNoParentsRequested,
		"errServingCancelled":   errServingCancelled,
	})
}
