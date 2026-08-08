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
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	avacommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

func TestResponder(t *testing.T) {
	tests := []struct {
		name       string
		chainLen   int
		numParents uint32
		noBlocks   bool
		cancelCtx  bool
		wantBlocks int
		wantErr    *avacommon.AppError
	}{
		{
			name:       "returns requested parents tip-first",
			chainLen:   10,
			numParents: 5,
			wantBlocks: 5,
		},
		{
			name:       "includes genesis then stops",
			chainLen:   5,
			numParents: 100, // more than the chain length
			wantBlocks: 6,
		},
		{
			name:       "caps parents at max",
			chainLen:   int(maxParentsPerRequest) + 10,
			numParents: uint32(maxParentsPerRequest) + 50,
			wantBlocks: int(maxParentsPerRequest),
		},
		{
			name:       "missing block rejected",
			noBlocks:   true,
			numParents: 1,
			wantErr:    errBlocksNotFound,
		},
		{
			name:       "cancelled context rejected",
			chainLen:   50,
			numParents: 10,
			cancelCtx:  true,
			wantErr:    errServingCancelled,
		},
		{
			name:       "zero parents rejected",
			chainLen:   10,
			numParents: 0,
			wantErr:    errNoParentsRequested,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var blocks []*types.Block
			if !tt.noBlocks {
				blocks = synctest.MakeChain(t, tt.chainLen)
			}
			r := newResponder(logging.NoLog{}, synctest.NewBlockMap(blocks))

			ctx := t.Context()
			if tt.cancelCtx {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel() // cancel before the responder runs
			}

			height := uint64(10)
			if len(blocks) > 0 {
				height = blocks[len(blocks)-1].NumberU64()
			}

			resp, err := r.Respond(ctx, ids.GenerateTestNodeID(), &syncpb.GetBlockRequest{
				Height:     height,
				NumParents: tt.numParents,
			})
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Nil(t, resp, "a rejected request carries no response")
				return
			}
			require.Nil(t, err)
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

func TestResponder_MaxResponseBytes(t *testing.T) {
	blocks := synctest.MakeChain(t, 5)
	tip := blocks[len(blocks)-1]
	oneBlock := len(encodeBlock(t, tip))

	tests := []struct {
		name       string
		budget     int
		wantBlocks int
	}{
		{
			name:       "budget fits three blocks",
			budget:     3 * oneBlock,
			wantBlocks: 3,
		},
		{
			name:       "oversized block served alone",
			budget:     oneBlock / 2,
			wantBlocks: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newResponder(logging.NoLog{}, synctest.NewBlockMap(blocks), WithMaxResponseBytes(tt.budget))

			resp, err := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetBlockRequest{
				Height:     tip.NumberU64(),
				NumParents: 5,
			})
			require.Nil(t, err)
			require.Len(t, resp.Blocks, tt.wantBlocks)
		})
	}
}
