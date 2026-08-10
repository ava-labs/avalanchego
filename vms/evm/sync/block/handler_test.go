// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"testing"

	"github.com/ava-labs/libevm/common"
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
		{
			name:       "budget fits three blocks",
			chainLen:   10,
			numParents: 5,
			budget:     3 * blockBytes,
			wantBlocks: 3,
		},
		{
			name:       "oversized block served alone",
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
			r := newResponder(logging.NoLog{}, synctest.NewBlockMap(blocks), opts...)

			ctx := t.Context()
			if tt.cancelCtx {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel() // cancel before the responder runs
			}

			var (
				hash   = common.Hash{0xde, 0xad}
				height = uint64(10)
			)
			if len(blocks) > 0 {
				tip := blocks[len(blocks)-1]
				hash, height = tip.Hash(), tip.NumberU64()
			}

			resp, err := r.Respond(ctx, ids.GenerateTestNodeID(), &syncpb.GetBlockRequest{
				Hash:       hash.Bytes(),
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
