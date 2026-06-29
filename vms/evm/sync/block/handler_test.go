// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block_test

import (
	"context"
	"testing"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/block"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

func TestHandler_RoundTrip(t *testing.T) {
	wantResp := &syncpb.GetBlockResponse{
		Blocks: [][]byte{{0xaa}, {0xbb}, {0xcc}},
	}
	responder := &synctest.FakeBlockResponder{Resp: wantResp}
	h := block.NewHandler(logging.NoLog{}, responder)

	req := &syncpb.GetBlockRequest{
		Height:     42,
		NumParents: 3,
	}
	respBytes, appErr := h.AppRequest(t.Context(), ids.GenerateTestNodeID(), time.Time{}, synctest.MustMarshal(t, req))
	require.Nil(t, appErr)

	got := &syncpb.GetBlockResponse{}
	require.NoError(t, proto.Unmarshal(respBytes, got))
	require.Empty(t, cmp.Diff(wantResp, got, protocmp.Transform()))
	require.Empty(t, cmp.Diff(req, responder.GotReq, protocmp.Transform()))
}

func TestResponder_ReturnsRequestedParents(t *testing.T) {
	blocks := synctest.MakeChain(t, 10)
	provider := synctest.NewBlockMap(blocks)
	r := block.NewResponder(provider)

	tip := blocks[len(blocks)-1]
	resp, err := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetBlockRequest{
		Height:     tip.NumberU64(),
		NumParents: 5,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Blocks, 5)

	// Blocks come back tip-first, walking parents.
	for i, raw := range resp.Blocks {
		var b types.Block
		require.NoError(t, rlp.DecodeBytes(raw, &b))
		want := blocks[len(blocks)-1-i]
		require.Equal(t, want.Hash(), b.Hash())
	}
}

func TestResponder_StopsAtGenesis(t *testing.T) {
	blocks := synctest.MakeChain(t, 5)
	provider := synctest.NewBlockMap(blocks)
	r := block.NewResponder(provider)

	tip := blocks[len(blocks)-1]
	resp, err := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetBlockRequest{
		Height:     tip.NumberU64(),
		NumParents: 100, // more than the chain length
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	// Genesis is included. The walk stops on the iteration after it,
	// when hash becomes the all-zero ParentHash of genesis.
	require.Len(t, resp.Blocks, 6)
}

func TestResponder_MissingBlockDrops(t *testing.T) {
	provider := synctest.NewBlockMap(nil)
	r := block.NewResponder(provider)

	resp, err := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetBlockRequest{
		Height:     10,
		NumParents: 1,
	})
	require.NoError(t, err)
	require.Nil(t, resp)
}

func TestResponder_ParentsCappedAtMax(t *testing.T) {
	blocks := synctest.MakeChain(t, int(block.MaxParentsPerRequest)+10)
	provider := synctest.NewBlockMap(blocks)
	r := block.NewResponder(provider)

	tip := blocks[len(blocks)-1]
	resp, err := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetBlockRequest{
		Height:     tip.NumberU64(),
		NumParents: uint32(block.MaxParentsPerRequest) + 50,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Blocks, int(block.MaxParentsPerRequest))
}

func TestResponder_CtxCancelledDrops(t *testing.T) {
	blocks := synctest.MakeChain(t, 50)
	provider := synctest.NewBlockMap(blocks)
	r := block.NewResponder(provider)

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel before the responder runs

	tip := blocks[len(blocks)-1]
	resp, err := r.Respond(ctx, ids.GenerateTestNodeID(), &syncpb.GetBlockRequest{
		Height:     tip.NumberU64(),
		NumParents: 10,
	})
	require.NoError(t, err)
	// Loop bails on the first iteration because ctx is already done.
	// No blocks were appended, so the responder drops.
	require.Nil(t, resp)
}
