// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/trie"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/evm/sync/evmstate"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

func TestHandler_RoundTrip(t *testing.T) {
	wantResp := &syncpb.GetLeafResponse{
		Keys:      [][]byte{{0x01}, {0x02}},
		Values:    [][]byte{{0xaa}, {0xbb}},
		ProofVals: [][]byte{{0xcc}},
	}
	responder := &synctest.FakeLeafResponder{Resp: wantResp}
	h := evmstate.NewHandler(responder)

	req := &syncpb.GetLeafRequest{
		RootHash:    []byte{0xde, 0xad},
		AccountHash: []byte{0xbe, 0xef},
		StartKey:    []byte{0x10},
		EndKey:      []byte{0x20},
		KeyLimit:    16,
		NodeType:    syncpb.EVMNodeType_EVM_NODE_TYPE_STATE_TRIE,
	}
	respBytes, appErr := h.AppRequest(t.Context(), ids.GenerateTestNodeID(), time.Time{}, synctest.MustMarshal(t, req))
	require.Nil(t, appErr)

	got := &syncpb.GetLeafResponse{}
	require.NoError(t, proto.Unmarshal(respBytes, got))
	require.Empty(t, cmp.Diff(wantResp, got, protocmp.Transform()))
	require.Empty(t, cmp.Diff(req, responder.GotReq, protocmp.Transform()))
}

func TestHandler_FailurePaths(t *testing.T) {
	tests := []struct {
		name       string
		resp       *syncpb.GetLeafResponse
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
			responder := &synctest.FakeLeafResponder{Resp: tt.resp, Err: tt.err}
			h := evmstate.NewHandler(responder)

			respBytes, appErr := h.AppRequest(t.Context(), ids.GenerateTestNodeID(), time.Time{}, synctest.MustMarshal(t, &syncpb.GetLeafRequest{}))
			if tt.wantAppErr {
				require.NotNil(t, appErr)
			} else {
				require.Nil(t, appErr)
			}
			require.Empty(t, respBytes)
		})
	}
}

func TestHandler_MalformedRequestBytes(t *testing.T) {
	responder := &synctest.FakeLeafResponder{}
	h := evmstate.NewHandler(responder)

	respBytes, appErr := h.AppRequest(t.Context(), ids.GenerateTestNodeID(), time.Time{}, []byte{0xff, 0xff})
	require.Nil(t, respBytes)
	require.NotNil(t, appErr)
	require.Nil(t, responder.GotReq, "responder must not be invoked on malformed request")
}

func TestResponder_ValidationDrops(t *testing.T) {
	t.Parallel()
	trieDB := synctest.NewTrieDB()
	root, _, _ := synctest.FillTrie(t, trieDB, 10)

	tests := []struct {
		name string
		req  *syncpb.GetLeafRequest
	}{
		{
			name: "NodeType unspecified",
			req: &syncpb.GetLeafRequest{
				RootHash: root.Bytes(),
				KeyLimit: 10,
				NodeType: syncpb.EVMNodeType_EVM_NODE_TYPE_UNSPECIFIED,
			},
		},
		{
			name: "NodeType atomic trie",
			req: &syncpb.GetLeafRequest{
				RootHash: root.Bytes(),
				KeyLimit: 10,
				NodeType: syncpb.EVMNodeType_EVM_NODE_TYPE_ATOMIC_TRIE,
			},
		},
		{
			name: "zero KeyLimit",
			req: &syncpb.GetLeafRequest{
				RootHash: root.Bytes(),
				KeyLimit: 0,
				NodeType: syncpb.EVMNodeType_EVM_NODE_TYPE_STATE_TRIE,
			},
		},
		{
			name: "KeyLimit overflows uint16",
			req: &syncpb.GetLeafRequest{
				RootHash: root.Bytes(),
				KeyLimit: uint32(^uint16(0)) + 1,
				NodeType: syncpb.EVMNodeType_EVM_NODE_TYPE_STATE_TRIE,
			},
		},
		{
			name: "StartKey > EndKey",
			req: &syncpb.GetLeafRequest{
				RootHash: root.Bytes(),
				StartKey: bytes.Repeat([]byte{0xff}, common.HashLength),
				EndKey:   bytes.Repeat([]byte{0x00}, common.HashLength),
				KeyLimit: 10,
				NodeType: syncpb.EVMNodeType_EVM_NODE_TYPE_STATE_TRIE,
			},
		},
		{
			name: "StartKey wrong length",
			req: &syncpb.GetLeafRequest{
				RootHash: root.Bytes(),
				StartKey: []byte{0x01, 0x02},
				KeyLimit: 10,
				NodeType: syncpb.EVMNodeType_EVM_NODE_TYPE_STATE_TRIE,
			},
		},
		{
			name: "RootHash empty",
			req: &syncpb.GetLeafRequest{
				RootHash: common.Hash{}.Bytes(),
				KeyLimit: 10,
				NodeType: syncpb.EVMNodeType_EVM_NODE_TYPE_STATE_TRIE,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := &synctest.LeafRecorder{}
			r := evmstate.NewResponder(trieDB, common.HashLength, nil, stats)
			resp, err := r.Respond(t.Context(), ids.GenerateTestNodeID(), tt.req)
			require.NoError(t, err)
			require.Nil(t, resp)
			require.Positive(t, stats.Invalid)
		})
	}
}

func TestResponder_MissingRootDrops(t *testing.T) {
	t.Parallel()
	trieDB := synctest.NewTrieDB()
	stats := &synctest.LeafRecorder{}
	r := evmstate.NewResponder(trieDB, common.HashLength, nil, stats)

	resp, err := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{
		RootHash: bytes.Repeat([]byte{0xab}, common.HashLength),
		KeyLimit: 10,
		NodeType: syncpb.EVMNodeType_EVM_NODE_TYPE_STATE_TRIE,
	})
	require.NoError(t, err)
	require.Nil(t, resp)
	require.Equal(t, uint32(1), stats.MissingRoot)
}

func TestResponder_TrieRoundTrip(t *testing.T) {
	t.Parallel()
	trieDB := synctest.NewTrieDB()
	root, keys, vals := synctest.FillTrie(t, trieDB, 50)

	stats := &synctest.LeafRecorder{}
	r := evmstate.NewResponder(trieDB, common.HashLength, nil, stats)

	resp, err := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{
		RootHash: root.Bytes(),
		KeyLimit: uint32(len(keys)),
		NodeType: syncpb.EVMNodeType_EVM_NODE_TYPE_STATE_TRIE,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, keys, resp.Keys)
	require.Equal(t, vals, resp.Values)
	// Whole-trie response from the start: no proof needed.
	require.Empty(t, resp.ProofVals)
	require.Equal(t, uint32(1), stats.Requests)
	require.Equal(t, uint16(len(keys)), stats.LeafReturned)
}

func TestResponder_PartialRangeIncludesProof(t *testing.T) {
	t.Parallel()
	trieDB := synctest.NewTrieDB()
	root, keys, vals := synctest.FillTrie(t, trieDB, 50)

	r := evmstate.NewResponder(trieDB, common.HashLength, nil, &synctest.LeafRecorder{})
	limit := uint32(20)

	resp, err := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{
		RootHash: root.Bytes(),
		KeyLimit: limit,
		NodeType: syncpb.EVMNodeType_EVM_NODE_TYPE_STATE_TRIE,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Keys, int(limit))
	require.Len(t, resp.Values, int(limit))
	// Partial response must include a proof so the client can verify
	// the right edge.
	require.NotEmpty(t, resp.ProofVals)
	require.Equal(t, keys[:limit], resp.Keys)
	require.Equal(t, vals[:limit], resp.Values)
}

func TestResponder_SnapshotFastPathSucceeds(t *testing.T) {
	t.Parallel()
	trieDB := synctest.NewTrieDB()
	root, keys, vals := synctest.FillTrie(t, trieDB, 30)

	// Build a snapshot that mirrors the trie exactly so the fast path
	// validates on the first try.
	pairs := make([]synctest.StaticPair, len(keys))
	for i := range keys {
		pairs[i] = synctest.StaticPair{K: keys[i], V: vals[i]}
	}
	snap := &synctest.StaticSnapshot{Pairs: map[common.Hash][]synctest.StaticPair{
		{}: pairs,
	}}

	stats := &synctest.LeafRecorder{}
	r := evmstate.NewResponder(trieDB, common.HashLength, snap, stats)
	resp, err := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{
		RootHash: root.Bytes(),
		KeyLimit: uint32(len(keys)),
		NodeType: syncpb.EVMNodeType_EVM_NODE_TYPE_STATE_TRIE,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, keys, resp.Keys)
	require.Equal(t, vals, resp.Values)
	require.Equal(t, uint32(1), stats.SnapAttempts)
	require.Equal(t, uint32(1), stats.SnapSuccess)
}

func TestResponder_SnapshotFallsBackToTrie(t *testing.T) {
	t.Parallel()
	trieDB := synctest.NewTrieDB()
	root, keys, vals := synctest.FillTrie(t, trieDB, 30)

	// Snapshot returns a key that is NOT in the trie (corrupted).
	bogusKey := make([]byte, common.HashLength)
	bogusKey[0] = 0xff
	snap := &synctest.StaticSnapshot{Pairs: map[common.Hash][]synctest.StaticPair{
		{}: {{K: bogusKey, V: []byte("bogus")}},
	}}

	stats := &synctest.LeafRecorder{}
	r := evmstate.NewResponder(trieDB, common.HashLength, snap, stats)
	resp, err := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{
		RootHash: root.Bytes(),
		KeyLimit: uint32(len(keys)),
		NodeType: syncpb.EVMNodeType_EVM_NODE_TYPE_STATE_TRIE,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	// Even with a bad snapshot, the trie fallback should produce the
	// correct keys/values.
	require.Equal(t, keys, resp.Keys)
	require.Equal(t, vals, resp.Values)
	// Snapshot was attempted but did not succeed end-to-end.
	require.Equal(t, uint32(1), stats.SnapAttempts)
}

func TestResponder_CorruptedTrieDrops(t *testing.T) {
	t.Parallel()
	trieDB, disk := synctest.NewTrieDBWithDisk()
	root, keys, _ := synctest.FillTrie(t, trieDB, 50)

	// Open the trie and delete every 2nd node so proof generation
	// fails when the responder tries to construct a range proof.
	tr, err := trie.New(trie.TrieID(root), trieDB)
	require.NoError(t, err)
	synctest.CorruptTrie(t, disk, tr, 2)

	stats := &synctest.LeafRecorder{}
	r := evmstate.NewResponder(trieDB, common.HashLength, nil, stats)

	// Request a partial range so the responder is forced to generate a
	// proof (which will fail because trie nodes are missing).
	resp, err := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{
		RootHash: root.Bytes(),
		KeyLimit: uint32(len(keys) / 2),
		NodeType: syncpb.EVMNodeType_EVM_NODE_TYPE_STATE_TRIE,
	})
	require.NoError(t, err)
	require.Nil(t, resp)
	require.Positive(t, stats.ProofErrors+stats.TrieErrors, "expected a trie or proof error to be recorded")
}

func TestResponder_CtxCancelledDrops(t *testing.T) {
	t.Parallel()
	trieDB := synctest.NewTrieDB()
	root, _, _ := synctest.FillTrie(t, trieDB, 10)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	r := evmstate.NewResponder(trieDB, common.HashLength, nil, &synctest.LeafRecorder{})
	resp, err := r.Respond(ctx, ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{
		RootHash: root.Bytes(),
		KeyLimit: 10,
		NodeType: syncpb.EVMNodeType_EVM_NODE_TYPE_STATE_TRIE,
	})
	require.NoError(t, err)
	// Ctx is cancelled before any leaves are read, so the responder drops.
	require.Nil(t, resp)
}
