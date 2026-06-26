// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

// assertContract runs the full shell contract for one concrete RPC type pair:
// malformed input, drop, deliver, app-error passthrough, and server-fault
// collapse. The shell has no type-specific branches, so each RPC type is
// expected to behave identically.
func assertContract[Req, Resp handlers.ProtoMessage](t *testing.T, newReq func() Req, req Req, resp Resp) {
	nodeID := ids.GenerateTestNodeID()
	reqBytes := synctest.MustMarshal(t, req)

	t.Run("malformed request", func(t *testing.T) {
		t.Parallel()

		r := &synctest.FakeResponder[Req, Resp]{}
		h := handlers.NewHandler(logging.NoLog{}, newReq, r)

		respBytes, appErr := h.AppRequest(t.Context(), nodeID, time.Time{}, []byte{0xff, 0xff})
		require.Nil(t, respBytes)
		require.Equal(t, p2p.ErrUnexpected.Code, appErr.Code)
		require.Equal(t, "malformed proto request", appErr.Message)
		require.Nil(t, r.GotReq, "responder must not be invoked on malformed request")
	})

	t.Run("zero response drops", func(t *testing.T) {
		t.Parallel()

		r := &synctest.FakeResponder[Req, Resp]{}
		h := handlers.NewHandler(logging.NoLog{}, newReq, r)

		respBytes, appErr := h.AppRequest(t.Context(), nodeID, time.Time{}, reqBytes)
		require.Nil(t, appErr)
		require.Nil(t, respBytes)
		require.Equal(t, reqBytes, synctest.MustMarshal(t, r.GotReq), "request decoded and passed to responder")
	})

	t.Run("response is marshaled", func(t *testing.T) {
		t.Parallel()

		r := &synctest.FakeResponder[Req, Resp]{Resp: resp}
		h := handlers.NewHandler(logging.NoLog{}, newReq, r)

		respBytes, appErr := h.AppRequest(t.Context(), nodeID, time.Time{}, reqBytes)
		require.Nil(t, appErr)
		require.Equal(t, synctest.MustMarshal(t, resp), respBytes)
	})

	t.Run("app error surfaces unchanged", func(t *testing.T) {
		t.Parallel()

		requestErr := &common.AppError{Code: 7, Message: "unknown request"}
		r := &synctest.FakeResponder[Req, Resp]{Err: requestErr}
		h := handlers.NewHandler(logging.NoLog{}, newReq, r)

		respBytes, appErr := h.AppRequest(t.Context(), nodeID, time.Time{}, reqBytes)
		require.Nil(t, respBytes)
		require.Equal(t, requestErr.Code, appErr.Code)
		require.Equal(t, requestErr.Message, appErr.Message)
	})

	t.Run("server fault becomes ErrUnexpected", func(t *testing.T) {
		t.Parallel()

		r := &synctest.FakeResponder[Req, Resp]{Err: errors.New("boom")}
		h := handlers.NewHandler(logging.NoLog{}, newReq, r)

		respBytes, appErr := h.AppRequest(t.Context(), nodeID, time.Time{}, reqBytes)
		require.Nil(t, respBytes)
		require.Equal(t, p2p.ErrUnexpected.Code, appErr.Code)
		require.Equal(t, p2p.ErrUnexpected.Message, appErr.Message)
	})
}

func TestAppRequest(t *testing.T) {
	t.Run("GetBlock", func(t *testing.T) {
		assertContract(t,
			func() *syncpb.GetBlockRequest { return &syncpb.GetBlockRequest{} },
			&syncpb.GetBlockRequest{Height: 7, NumParents: 2},
			&syncpb.GetBlockResponse{Blocks: [][]byte{{0x01}}},
		)
	})
	t.Run("GetCode", func(t *testing.T) {
		assertContract(t,
			func() *syncpb.GetCodeRequest { return &syncpb.GetCodeRequest{} },
			&syncpb.GetCodeRequest{Hashes: [][]byte{{0x01}}},
			&syncpb.GetCodeResponse{Data: [][]byte{{0x02}}},
		)
	})
	t.Run("GetLeaf", func(t *testing.T) {
		assertContract(t,
			func() *syncpb.GetLeafRequest { return &syncpb.GetLeafRequest{} },
			&syncpb.GetLeafRequest{RootHash: []byte{0x01}},
			&syncpb.GetLeafResponse{Keys: [][]byte{{0x02}}},
		)
	})
}
