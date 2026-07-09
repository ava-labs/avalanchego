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
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

func TestResponder(t *testing.T) {
	tests := []struct {
		name       string
		chainLen   int
		numParents uint32
		noBlocks   bool
		cancelCtx  bool
		wantBlocks int
		wantDrop   bool
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
			chainLen:   int(MaxParentsPerRequest) + 10,
			numParents: uint32(MaxParentsPerRequest) + 50,
			wantBlocks: int(MaxParentsPerRequest),
		},
		{
			name:       "missing block drops",
			noBlocks:   true,
			numParents: 1,
			wantDrop:   true,
		},
		{
			name:       "cancelled context drops",
			chainLen:   50,
			numParents: 10,
			cancelCtx:  true,
			wantDrop:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var blocks []*types.Block
			if !tt.noBlocks {
				blocks = synctest.MakeChain(t, tt.chainLen)
			}
			r := newResponder(synctest.NewBlockMap(blocks))

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
			require.NoError(t, err)

			if tt.wantDrop {
				require.Nil(t, resp)
				return
			}
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
