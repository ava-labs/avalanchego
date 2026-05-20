// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers_test

import (
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

func TestCodeHandler_RoundTrip(t *testing.T) {
	wantResp := &syncpb.GetCodeResponse{
		Data: [][]byte{{0xaa, 0xbb}, {0xcc, 0xdd}},
	}
	responder := &synctest.FakeCodeResponder{Resp: wantResp}
	h := handlers.NewCodeHandler(responder)

	req := &syncpb.GetCodeRequest{
		Hashes: [][]byte{{0x01}, {0x02}},
	}
	respBytes, appErr := h.AppRequest(t.Context(), ids.GenerateTestNodeID(), time.Time{}, synctest.MustMarshal(t, req))
	require.Nil(t, appErr)

	got := &syncpb.GetCodeResponse{}
	require.NoError(t, proto.Unmarshal(respBytes, got))
	require.Empty(t, cmp.Diff(wantResp, got, protocmp.Transform()))
	require.Empty(t, cmp.Diff(req, responder.GotReq, protocmp.Transform()))
}

func TestCodeHandler_FailurePaths(t *testing.T) {
	tests := []struct {
		name       string
		resp       *syncpb.GetCodeResponse
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
			responder := &synctest.FakeCodeResponder{Resp: tt.resp, Err: tt.err}
			h := handlers.NewCodeHandler(responder)

			respBytes, appErr := h.AppRequest(t.Context(), ids.GenerateTestNodeID(), time.Time{}, synctest.MustMarshal(t, &syncpb.GetCodeRequest{}))
			if tt.wantAppErr {
				require.NotNil(t, appErr)
			} else {
				require.Nil(t, appErr)
			}
			require.Empty(t, respBytes)
		})
	}
}

func TestCodeHandler_MalformedRequestBytes(t *testing.T) {
	responder := &synctest.FakeCodeResponder{}
	h := handlers.NewCodeHandler(responder)

	respBytes, appErr := h.AppRequest(t.Context(), ids.GenerateTestNodeID(), time.Time{}, []byte{0xff, 0xff})
	require.Nil(t, respBytes)
	require.NotNil(t, appErr)
	require.Nil(t, responder.GotReq, "responder must not be invoked on malformed request")
}

func TestCodeResponder(t *testing.T) {
	t.Parallel()

	db := memorydb.New()
	codeBytes := []byte("contract bytecode")
	codeHash := crypto.Keccak256Hash(codeBytes)
	rawdb.WriteCode(db, codeHash, codeBytes)

	// A second code blob to test multi-hash requests.
	other := make([]byte, 256)
	_, err := rand.Read(other)
	require.NoError(t, err)
	otherHash := crypto.Keccak256Hash(other)
	rawdb.WriteCode(db, otherHash, other)

	tests := []struct {
		name      string
		hashes    []common.Hash
		wantData  [][]byte
		wantDrop  bool
		wantStats synctest.CodeRecorder
	}{
		{
			name:      "single hash",
			hashes:    []common.Hash{codeHash},
			wantData:  [][]byte{codeBytes},
			wantStats: synctest.CodeRecorder{Requests: 1, CodeBytesReturned: uint32(len(codeBytes))},
		},
		{
			name:      "multiple hashes preserve order",
			hashes:    []common.Hash{codeHash, otherHash},
			wantData:  [][]byte{codeBytes, other},
			wantStats: synctest.CodeRecorder{Requests: 1, CodeBytesReturned: uint32(len(codeBytes) + len(other))},
		},
		{
			name:      "missing hash drops",
			hashes:    []common.Hash{{0xde, 0xad}},
			wantDrop:  true,
			wantStats: synctest.CodeRecorder{Requests: 1, MissingHash: 1},
		},
		{
			name:      "duplicate hashes drop",
			hashes:    []common.Hash{codeHash, codeHash},
			wantDrop:  true,
			wantStats: synctest.CodeRecorder{Requests: 1, DuplicateHashes: 1},
		},
		{
			name:      "too many hashes drops",
			hashes:    []common.Hash{{1}, {2}, {3}, {4}, {5}, {6}},
			wantDrop:  true,
			wantStats: synctest.CodeRecorder{Requests: 1, TooManyHashes: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := &synctest.CodeRecorder{}
			r := handlers.NewCodeResponder(db, stats)

			rawHashes := make([][]byte, len(tt.hashes))
			for i, h := range tt.hashes {
				rawHashes[i] = h.Bytes()
			}
			resp, err := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetCodeRequest{Hashes: rawHashes})
			require.NoError(t, err)

			if tt.wantDrop {
				require.Nil(t, resp)
			} else {
				require.NotNil(t, resp)
				require.Equal(t, tt.wantData, resp.Data)
			}
			require.Equal(t, tt.wantStats, *stats)
		})
	}
}
