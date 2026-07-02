// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate_test

import (
	"bytes"
	"context"
	"math"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/trie"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
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
	h := evmstate.NewHandler(logging.NoLog{}, responder)

	req := &syncpb.GetLeafRequest{
		RootHash:    []byte{0xde, 0xad},
		AccountHash: []byte{0xbe, 0xef},
		StartKey:    []byte{0x10},
		EndKey:      []byte{0x20},
		KeyLimit:    16,
	}
	respBytes, appErr := h.AppRequest(t.Context(), ids.GenerateTestNodeID(), time.Time{}, synctest.MustMarshal(t, req))
	require.Nil(t, appErr)

	got := &syncpb.GetLeafResponse{}
	require.NoError(t, proto.Unmarshal(respBytes, got))
	require.Empty(t, cmp.Diff(wantResp, got, protocmp.Transform()))
	require.Empty(t, cmp.Diff(req, responder.GotReq, protocmp.Transform()))
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
			name: "zero KeyLimit",
			req: &syncpb.GetLeafRequest{
				RootHash: root.Bytes(),
				KeyLimit: 0,
			},
		},
		{
			name: "KeyLimit overflows uint16",
			req: &syncpb.GetLeafRequest{
				RootHash: root.Bytes(),
				KeyLimit: math.MaxUint16 + 1,
			},
		},
		{
			name: "StartKey > EndKey",
			req: &syncpb.GetLeafRequest{
				RootHash: root.Bytes(),
				StartKey: bytes.Repeat([]byte{0xff}, common.HashLength),
				EndKey:   bytes.Repeat([]byte{0x00}, common.HashLength),
				KeyLimit: 10,
			},
		},
		{
			name: "StartKey wrong length",
			req: &syncpb.GetLeafRequest{
				RootHash: root.Bytes(),
				StartKey: []byte{0x01, 0x02},
				KeyLimit: 10,
			},
		},
		{
			name: "RootHash empty",
			req: &syncpb.GetLeafRequest{
				RootHash: common.Hash{}.Bytes(),
				KeyLimit: 10,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := evmstate.NewResponder(trieDB, common.HashLength, nil)
			resp, err := r.Respond(t.Context(), ids.GenerateTestNodeID(), tt.req)
			require.NoError(t, err)
			require.Nil(t, resp)
		})
	}
}

func TestResponder(t *testing.T) {
	t.Parallel()

	const numKeys = 50

	tests := []struct {
		name        string
		limit       uint32
		corruptTrie bool
		badRoot     bool
		cancelCtx   bool
		wantDrop    bool
	}{
		{
			name:  "whole trie has no proof",
			limit: numKeys,
		},
		{
			name:  "partial range includes proof",
			limit: numKeys / 2,
		},
		{
			name:     "missing root drops",
			limit:    numKeys,
			badRoot:  true,
			wantDrop: true,
		},
		{
			// A partial range reaches the proof step, which fails on the
			// corrupted trie.
			name:        "corrupted trie drops",
			limit:       numKeys / 2,
			corruptTrie: true,
			wantDrop:    true,
		},
		{
			name:      "cancelled context drops",
			limit:     numKeys,
			cancelCtx: true,
			wantDrop:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			trieDB, disk := synctest.NewTrieDBWithDisk()
			root, keys, vals := synctest.FillTrie(t, trieDB, numKeys)

			if tt.corruptTrie {
				// Corrupt nodes so proof building fails.
				tr, err := trie.New(trie.TrieID(root), trieDB)
				require.NoError(t, err)
				synctest.CorruptTrie(t, disk, tr, 2)
			}

			r := evmstate.NewResponder(trieDB, common.HashLength, nil)

			ctx := t.Context()
			if tt.cancelCtx {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			rootHash := root.Bytes()
			if tt.badRoot {
				rootHash = bytes.Repeat([]byte{0xab}, common.HashLength)
			}

			resp, err := r.Respond(ctx, ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{
				RootHash: rootHash,
				KeyLimit: tt.limit,
			})
			require.NoError(t, err)

			if tt.wantDrop {
				require.Nil(t, resp)
				return
			}
			require.NotNil(t, resp)

			n := int(tt.limit)
			require.Equal(t, keys[:n], resp.Keys)
			require.Equal(t, vals[:n], resp.Values)
			// Partial ranges carry a proof, whole-trie responses don't.
			if n < numKeys {
				require.NotEmpty(t, resp.ProofVals)
			} else {
				require.Empty(t, resp.ProofVals)
			}
		})
	}
}

func TestResponder_BoundedRange(t *testing.T) {
	t.Parallel()
	trieDB := synctest.NewTrieDB()
	root, keys, vals := synctest.FillTrie(t, trieDB, 50)

	r := evmstate.NewResponder(trieDB, common.HashLength, nil)
	resp, err := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{
		RootHash: root.Bytes(),
		StartKey: keys[10],
		EndKey:   keys[30],
		KeyLimit: uint32(len(keys)),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	// EndKey is inclusive.
	require.Equal(t, keys[10:31], resp.Keys)
	require.Equal(t, vals[10:31], resp.Values)
	require.NotEmpty(t, resp.ProofVals)
}

func TestResponder_Snapshot(t *testing.T) {
	t.Parallel()

	// Segment length is 64, so 130 keys spans three segments.
	const numKeys = 130

	tests := []struct {
		name string
		// Snapshot values in [corruptFrom, corruptTo) are wrong.
		corruptFrom int
		corruptTo   int
	}{
		{
			name: "fast path serves leaves",
		},
		{
			name:        "all invalid falls back to trie",
			corruptFrom: 0,
			corruptTo:   numKeys,
		},
		{
			name:        "slow path bridges an invalid middle segment",
			corruptFrom: 64,
			corruptTo:   128,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			trieDB := synctest.NewTrieDB()
			root, keys, vals := synctest.FillTrie(t, trieDB, numKeys)

			snap := synctest.MirrorSnapshot(keys, vals)
			for i := tt.corruptFrom; i < tt.corruptTo; i++ {
				snap.Pairs[common.Hash{}][i].V = bytes.Repeat([]byte{0xff}, common.HashLength)
			}

			r := evmstate.NewResponder(trieDB, common.HashLength, snap)
			requireServesWholeTrie(t, r, root, keys, vals)
		})
	}
}

// requireServesWholeTrie asserts a whole-trie request to r returns keys/vals.
func requireServesWholeTrie(t *testing.T, r evmstate.Responder, root common.Hash, keys, vals [][]byte) {
	t.Helper()
	resp, err := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{
		RootHash: root.Bytes(),
		KeyLimit: uint32(len(keys)),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, keys, resp.Keys)
	require.Equal(t, vals, resp.Values)
}
