// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

// assertContract runs the shell contract for one RPC type pair. The shell has
// no type-specific branches, so every RPC type must behave identically.
func assertContract[Req, Resp handlers.ProtoMessage](t *testing.T, req Req, resp Resp) {
	nodeID := ids.GenerateTestNodeID()
	reqBytes := synctest.MustMarshal(t, req)
	newReq := func() Req { return req.ProtoReflect().New().Interface().(Req) }

	t.Run("malformed request", func(t *testing.T) {
		t.Parallel()

		r := &synctest.FakeResponder[Req, Resp]{}
		h := handlers.NewHandler(logging.NoLog{}, newReq, r)

		respBytes, appErr := h.AppRequest(t.Context(), nodeID, time.Time{}, []byte{0xff, 0xff})
		require.Nil(t, respBytes)
		require.Equal(t, handlers.ErrMalformedRequest, appErr)
		require.Nil(t, r.GotReq, "responder must not be invoked on malformed request")
	})

	t.Run("zero response drops", func(t *testing.T) {
		t.Parallel()

		r := &synctest.FakeResponder[Req, Resp]{}
		h := handlers.NewHandler(logging.NoLog{}, newReq, r)

		respBytes, appErr := h.AppRequest(t.Context(), nodeID, time.Time{}, reqBytes)
		require.Nil(t, appErr)
		require.Nil(t, respBytes)
		assert.Empty(t, cmp.Diff(req, r.GotReq, protocmp.Transform()), "cmp.Diff(request, responder request)")
	})

	t.Run("response is marshaled", func(t *testing.T) {
		t.Parallel()

		r := &synctest.FakeResponder[Req, Resp]{Resp: resp}
		h := handlers.NewHandler(logging.NoLog{}, newReq, r)

		respBytes, appErr := h.AppRequest(t.Context(), nodeID, time.Time{}, reqBytes)
		require.Nil(t, appErr)

		got := resp.ProtoReflect().New().Interface()
		require.NoError(t, proto.Unmarshal(respBytes, got))
		assert.Empty(t, cmp.Diff(resp, got, protocmp.Transform()), "cmp.Diff(response, unmarshaled response)")
	})

	t.Run("app error surfaces unchanged", func(t *testing.T) {
		t.Parallel()

		requestErr := &common.AppError{Code: 7, Message: "unknown request"}
		r := &synctest.FakeResponder[Req, Resp]{Err: requestErr}
		h := handlers.NewHandler(logging.NoLog{}, newReq, r)

		respBytes, appErr := h.AppRequest(t.Context(), nodeID, time.Time{}, reqBytes)
		require.Nil(t, respBytes)
		require.Equal(t, requestErr, appErr)
	})

	t.Run("server fault becomes ErrUnexpected", func(t *testing.T) {
		t.Parallel()

		r := &synctest.FakeResponder[Req, Resp]{Err: errors.New("boom")}
		h := handlers.NewHandler(logging.NoLog{}, newReq, r)

		respBytes, appErr := h.AppRequest(t.Context(), nodeID, time.Time{}, reqBytes)
		require.Nil(t, respBytes)
		require.Equal(t, p2p.ErrUnexpected, appErr)
	})
}

func TestAppRequest(t *testing.T) {
	t.Run("GetBlock", func(t *testing.T) {
		assertContract(t,
			&syncpb.GetBlockRequest{Height: 7, NumParents: 2},
			&syncpb.GetBlockResponse{Blocks: [][]byte{{0x01}}},
		)
	})
	t.Run("GetCode", func(t *testing.T) {
		assertContract(t,
			&syncpb.GetCodeRequest{Hashes: [][]byte{{0x01}}},
			&syncpb.GetCodeResponse{Data: [][]byte{{0x02}}},
		)
	})
	t.Run("GetLeaf", func(t *testing.T) {
		assertContract(t,
			&syncpb.GetLeafRequest{RootHash: []byte{0x01}},
			&syncpb.GetLeafResponse{Keys: [][]byte{{0x02}}},
		)
	})
}
