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
	"github.com/ava-labs/avalanchego/vms/evm/sync/types"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

func assertContract[V any, Req types.ProtoMessage[V], Resp proto.Message](t *testing.T, req Req, resp Resp) {
	nodeID := ids.GenerateTestNodeID()
	reqBytes := synctest.MustMarshal(t, req)

	var noResp Resp
	requestErr := &common.AppError{Code: 7, Message: "unknown request"}

	tests := []struct {
		name         string
		respondWith  Resp
		respondErr   *common.AppError
		requestBytes []byte
		wantBytes    []byte
		wantErr      *common.AppError
		wantReached  bool
	}{
		{
			name:         "malformed request",
			requestBytes: []byte{0xff, 0xff},
			wantErr:      handlers.ErrMalformedRequest,
			wantReached:  false,
		},
		{
			// A typed nil is invalid, so it marshals to a nil buffer, not an
			// empty one.
			name:         "nil response yields a zero-length payload",
			respondWith:  noResp,
			requestBytes: reqBytes,
			wantBytes:    nil,
			wantReached:  true,
		},
		{
			name:         "response is marshaled",
			respondWith:  resp,
			requestBytes: reqBytes,
			wantBytes:    synctest.MustMarshal(t, resp),
			wantReached:  true,
		},
		{
			name:         "app error surfaces unchanged",
			respondErr:   requestErr,
			requestBytes: reqBytes,
			wantErr:      requestErr,
			wantReached:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &synctest.FakeResponder[Req, Resp]{Resp: tt.respondWith, Err: tt.respondErr}
			h := handlers.NewHandler(logging.NoLog{}, r)

			respBytes, appErr := h.AppRequest(t.Context(), nodeID, time.Time{}, tt.requestBytes)
			require.Equal(t, tt.wantErr, appErr)
			require.Equal(t, tt.wantBytes, respBytes)

			if !tt.wantReached {
				require.Nil(t, r.GotReq, "responder must not be invoked")
				return
			}
			assert.Empty(t, cmp.Diff(req, r.GotReq, protocmp.Transform()), "cmp.Diff(request, responder request)")
		})
	}
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

func TestErrorSentinels(t *testing.T) {
	synctest.RequireDistinctAppErrors(t, map[string]*common.AppError{
		"ErrMalformedRequest": handlers.ErrMalformedRequest,
		"ErrMarshalResponse":  handlers.ErrMarshalResponse,
	})
}

func TestFault(t *testing.T) {
	appErr := handlers.Fault(logging.NoLog{}, ids.GenerateTestNodeID(), errors.New("boom"))
	require.Equal(t, p2p.ErrUnexpected, appErr, "the peer learns nothing about the fault")
}
