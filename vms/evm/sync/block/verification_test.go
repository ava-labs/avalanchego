// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

func TestGetBlocks_RetriesUntilValid(t *testing.T) {
	// chain creates a genesis block and a single tip block.
	chain := synctest.MakeChain(t, 1)
	genesis := chain[0]
	tip := chain[1]

	goodResp := blockResponse(t, tip)
	// A block whose hash does not match the requested tip hash.
	mismatchResp := blockResponse(t, genesis)
	emptyResp := synctest.MustMarshal(t, &syncpb.GetBlockResponse{})

	tests := []struct {
		name    string
		badResp []byte
	}{
		{
			name:    "empty response is retried",
			badResp: emptyResp,
		},
		{
			name:    "hash mismatch is retried",
			badResp: mismatchResp,
		},
		{
			name:    "network error is retried",
			badResp: nil, // handler returns an AppError
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// responses served in order: bad first, then good.
			h := synctest.NewScriptedHandler([][]byte{tt.badResp, goodResp})

			net, tracker := synctest.NewLoopbackNetwork(t, p2p.EVMBlockRequestHandlerID, h)
			g := &getter{
				client: NewClient(net, tracker),
				log:    loggingtest.New(t, logging.Debug),
			}

			blocks, err := g.GetBlocks(t.Context(), tip.Hash(), tip.NumberU64(), 1)
			require.NoError(t, err)
			require.Len(t, blocks, 1)
			require.Equal(t, tip.Hash(), blocks[0].Hash())
			require.Equal(t, 2, h.Calls())
		})
	}
}

func TestGetBlocks_RetriesOnExtraCheck(t *testing.T) {
	chain := synctest.MakeChain(t, 1)
	tip := chain[1]

	// The handler always serves a valid response; only the extra check
	// gates acceptance.
	h := synctest.NewScriptedHandler([][]byte{blockResponse(t, tip)})
	net, tracker := synctest.NewLoopbackNetwork(t, p2p.EVMBlockRequestHandlerID, h)

	var (
		checks   int
		errExtra = errors.New("extra check failed")
	)
	g := &getter{
		client: NewClient(net, tracker),
		log:    loggingtest.New(t, logging.Debug),
		extraCheck: func(*types.Block) error {
			checks++
			if checks == 1 {
				return errExtra // reject the first otherwise-valid block
			}
			return nil
		},
	}

	blocks, err := g.GetBlocks(t.Context(), tip.Hash(), tip.NumberU64(), 1)
	require.NoError(t, err)
	require.Len(t, blocks, 1)
	require.Equal(t, tip.Hash(), blocks[0].Hash())

	require.Equal(t, 2, h.Calls())
	require.Equal(t, 2, checks)
}

// TestGetBlocks_ContextCancelled asserts that a cancelled context returns the
// context error early, before any request is sent.
func TestGetBlocks_ContextCancelled(t *testing.T) {
	chain := synctest.MakeChain(t, 0)
	tip := chain[0]

	h := synctest.NewScriptedHandler([][]byte{blockResponse(t, tip)})
	net, tracker := synctest.NewLoopbackNetwork(t, p2p.EVMBlockRequestHandlerID, h)
	g := &getter{
		client: NewClient(net, tracker),
		log:    loggingtest.New(t, logging.Debug),
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := g.GetBlocks(ctx, tip.Hash(), 0, 1)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, h.Calls())
}

func blockResponse(t *testing.T, b *types.Block) []byte {
	t.Helper()
	raw, err := rlp.EncodeToBytes(b)
	require.NoError(t, err)
	return synctest.MustMarshal(t, &syncpb.GetBlockResponse{Blocks: [][]byte{raw}})
}
