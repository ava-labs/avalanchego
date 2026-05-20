// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

func TestBlockHandler_RoundTrip(t *testing.T) {
	wantResp := &syncpb.GetBlockResponse{
		Blocks: [][]byte{{0xaa}, {0xbb}, {0xcc}},
	}
	responder := &synctest.FakeBlockResponder{Resp: wantResp}
	h := handlers.NewBlockHandler(responder)

	req := &syncpb.GetBlockRequest{
		Height:  42,
		Parents: 3,
	}
	respBytes, appErr := h.AppRequest(t.Context(), ids.GenerateTestNodeID(), time.Time{}, synctest.MustMarshal(t, req))
	require.Nil(t, appErr)

	got := &syncpb.GetBlockResponse{}
	require.NoError(t, proto.Unmarshal(respBytes, got))
	require.Empty(t, cmp.Diff(wantResp, got, protocmp.Transform()))
	require.Empty(t, cmp.Diff(req, responder.GotReq, protocmp.Transform()))
}

func TestBlockHandler_FailurePaths(t *testing.T) {
	tests := []struct {
		name       string
		resp       *syncpb.GetBlockResponse
		err        error
		wantAppErr bool
	}{
		{
			name: "inner returns nil drops the response",
		},
		{
			name:       "inner error surfaces as AppError",
			err:        errors.New("boom"),
			wantAppErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			responder := &synctest.FakeBlockResponder{Resp: tt.resp, Err: tt.err}
			h := handlers.NewBlockHandler(responder)

			respBytes, appErr := h.AppRequest(t.Context(), ids.GenerateTestNodeID(), time.Time{}, synctest.MustMarshal(t, &syncpb.GetBlockRequest{}))
			if tt.wantAppErr {
				require.NotNil(t, appErr)
			} else {
				require.Nil(t, appErr)
			}
			require.Empty(t, respBytes)
		})
	}
}

func TestBlockHandler_MalformedRequestBytes(t *testing.T) {
	responder := &synctest.FakeBlockResponder{}
	h := handlers.NewBlockHandler(responder)

	respBytes, appErr := h.AppRequest(t.Context(), ids.GenerateTestNodeID(), time.Time{}, []byte{0xff, 0xff})
	require.Nil(t, respBytes)
	require.NotNil(t, appErr)
	require.Nil(t, responder.GotReq, "responder must not be invoked on malformed request")
}

func TestBlockResponder_ReturnsRequestedParents(t *testing.T) {
	blocks := synctest.MakeChain(t, 10)
	provider := synctest.NewBlockMap(blocks)
	stats := &synctest.BlockRecorder{}
	r := handlers.NewBlockResponder(provider, stats)

	tip := blocks[len(blocks)-1]
	resp, err := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetBlockRequest{
		Height:  tip.NumberU64(),
		Parents: 5,
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
	require.Equal(t, uint32(1), stats.Requests)
	require.Equal(t, uint16(5), stats.BlocksReturned)
}

func TestBlockResponder_StopsAtGenesis(t *testing.T) {
	blocks := synctest.MakeChain(t, 5)
	provider := synctest.NewBlockMap(blocks)
	r := handlers.NewBlockResponder(provider, &synctest.BlockRecorder{})

	tip := blocks[len(blocks)-1]
	resp, err := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetBlockRequest{
		Height:  tip.NumberU64(),
		Parents: 100, // more than the chain length
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	// Genesis is included. The walk stops on the iteration after it,
	// when hash becomes the all-zero ParentHash of genesis.
	require.Len(t, resp.Blocks, 6)
}

func TestBlockResponder_MissingBlockDrops(t *testing.T) {
	provider := synctest.NewBlockMap(nil)
	stats := &synctest.BlockRecorder{}
	r := handlers.NewBlockResponder(provider, stats)

	resp, err := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetBlockRequest{
		Height:  10,
		Parents: 1,
	})
	require.NoError(t, err)
	require.Nil(t, resp)
	require.Equal(t, uint32(1), stats.MissingHash)
}

func TestBlockResponder_ParentsCappedAtMax(t *testing.T) {
	blocks := synctest.MakeChain(t, int(handlers.MaxParentsPerRequest)+10)
	provider := synctest.NewBlockMap(blocks)
	stats := &synctest.BlockRecorder{}
	r := handlers.NewBlockResponder(provider, stats)

	tip := blocks[len(blocks)-1]
	resp, err := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetBlockRequest{
		Height:  tip.NumberU64(),
		Parents: uint32(handlers.MaxParentsPerRequest) + 50,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Blocks, int(handlers.MaxParentsPerRequest))
}

func TestBlockResponder_CtxCancelledDrops(t *testing.T) {
	blocks := synctest.MakeChain(t, 50)
	provider := synctest.NewBlockMap(blocks)
	r := handlers.NewBlockResponder(provider, &synctest.BlockRecorder{})

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel before the responder runs

	tip := blocks[len(blocks)-1]
	resp, err := r.Respond(ctx, ids.GenerateTestNodeID(), &syncpb.GetBlockRequest{
		Height:  tip.NumberU64(),
		Parents: 10,
	})
	require.NoError(t, err)
	// Loop bails on the first iteration because ctx is already done.
	// No blocks were appended, so the responder drops.
	require.Nil(t, resp)
}
