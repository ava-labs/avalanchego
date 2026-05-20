// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate_test

import (
	"bytes"
	"context"
	"errors"
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

func TestResponder_Serves(t *testing.T) {
	t.Parallel()

	const numKeys = 50

	tests := []struct {
		name  string
		limit uint32
	}{
		{name: "whole trie has no proof", limit: numKeys},
		{name: "partial range includes proof", limit: numKeys / 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			trieDB := synctest.NewTrieDB()
			root, keys, vals := synctest.FillTrie(t, trieDB, numKeys)

			r := evmstate.NewResponder(trieDB, common.HashLength, nil)
			resp, err := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{
				RootHash: root.Bytes(),
				KeyLimit: tt.limit,
			})
			require.NoError(t, err)
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

func TestResponder_Drops(t *testing.T) {
	t.Parallel()

	const numKeys = 50

	tests := []struct {
		name        string
		limit       uint32
		badRoot     bool
		corruptTrie bool
		cancelCtx   bool
	}{
		{name: "missing root", limit: numKeys, badRoot: true},
		// A partial range reaches the proof step, which the corrupt trie fails.
		{name: "corrupted trie", limit: numKeys / 2, corruptTrie: true},
		{name: "cancelled context", limit: numKeys, cancelCtx: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			trieDB, disk := synctest.NewTrieDBWithDisk()
			root, _, _ := synctest.FillTrie(t, trieDB, numKeys)

			if tt.corruptTrie {
				tr, err := trie.New(trie.TrieID(root), trieDB)
				require.NoError(t, err)
				synctest.CorruptTrie(t, disk, tr, 2)
			}
			rootHash := root.Bytes()
			if tt.badRoot {
				rootHash = bytes.Repeat([]byte{0xab}, common.HashLength)
			}
			ctx := t.Context()
			if tt.cancelCtx {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			r := evmstate.NewResponder(trieDB, common.HashLength, nil)
			resp, err := r.Respond(ctx, ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{
				RootHash: rootHash,
				KeyLimit: tt.limit,
			})
			require.NoError(t, err)
			require.Nil(t, resp)
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

	// 130 accounts spans three 64-key segments.
	const numAccounts = 130

	tests := []struct {
		name string
		// Accounts in [corruptFrom, corruptTo) become a different valid account,
		// failing those segments. err makes the whole snapshot unavailable.
		corruptFrom int
		corruptTo   int
		err         bool
	}{
		{name: "fast path serves leaves"},
		{name: "slow path bridges an invalid middle segment", corruptFrom: 64, corruptTo: 128},
		{name: "all invalid falls back to trie", corruptFrom: 0, corruptTo: numAccounts},
		{name: "unavailable snapshot falls back to trie", err: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			trieDB := synctest.NewTrieDB()
			root, keys, vals, snap := synctest.FillAccountTrie(t, trieDB, numAccounts)
			for i := tt.corruptFrom; i < tt.corruptTo; i++ {
				snap.Accounts[i].V = snap.Accounts[0].V
			}
			if tt.err {
				snap.Err = errors.New("snapshot unavailable")
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
