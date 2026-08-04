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

func assertContract[Req, Resp proto.Message](t *testing.T, req Req, resp Resp) {
	nodeID := ids.GenerateTestNodeID()
	reqBytes := synctest.MustMarshal(t, req)
	newReq := func() Req { return req.ProtoReflect().New().Interface().(Req) }

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
			h := handlers.NewHandler(logging.NoLog{}, newReq, r)

			respBytes, appErr := h.AppRequest(t.Context(), nodeID, time.Time{}, tt.requestBytes)
			require.Equal(t, tt.wantErr, appErr)

			if tt.wantErr != nil {
				require.Nil(t, respBytes)
			} else {
				require.Equal(t, tt.wantBytes, respBytes)
			}

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
	// [common.AppError.Is] compares Code and nothing else.
	sentinels := map[string]*common.AppError{
		"ErrMalformedRequest": handlers.ErrMalformedRequest,
		"ErrMarshalResponse":  handlers.ErrMarshalResponse,
	}
	framework := []*common.AppError{
		p2p.ErrUnexpected,
		p2p.ErrUnregisteredHandler,
		p2p.ErrNotValidator,
		p2p.ErrThrottled,
		common.ErrUndefined,
		common.ErrTimeout,
	}

	// A code is the identity, the message is decoration. Each sentinel must:
	//   - be findable by its code
	//   - use a positive code, p2p owns the negatives and zero
	//   - not share a code with a framework error
	//   - not share a code with another sentinel
	seen := make(map[int32]string, len(sentinels))
	for name, sentinel := range sentinels {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, sentinel, &common.AppError{Code: sentinel.Code})
			require.Positive(t, sentinel.Code, "p2p and the engine own the non-positive codes")

			for _, f := range framework {
				require.NotErrorIs(t, sentinel, f)
			}
		})

		other, dup := seen[sentinel.Code]
		require.Falsef(t, dup, "%s and %s share code %d", name, other, sentinel.Code)
		seen[sentinel.Code] = name
	}
}

func TestFault(t *testing.T) {
	appErr := handlers.Fault(logging.NoLog{}, ids.GenerateTestNodeID(), errors.New("boom"))
	require.Equal(t, p2p.ErrUnexpected, appErr, "the peer learns nothing about the fault")
}
