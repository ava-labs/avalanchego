// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"bytes"
	"context"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

// serve registers the leaf handler for trieDB on a loopback network and returns
// a client bound to it, so a test drives both halves of the protocol.
func serve(t *testing.T, ctx context.Context, trieDB *triedb.Database, opts ...HandlerOption) *Client {
	t.Helper()
	log := loggingtest.New(t, logging.Debug)
	net, tracker := synctest.NewSelfNetwork(t, ctx, ids.GenerateTestNodeID())
	require.NoError(t, RegisterHandler(log, net, p2p.EVMLeafRequestHandlerID, trieDB, common.HashLength, opts...))
	return NewClient(log, net, p2p.EVMLeafRequestHandlerID, tracker)
}

// rawResponse fetches a range at the wire level, so a test can build cases from
// proofs the handler really produced.
func rawResponse(t *testing.T, ctx context.Context, c *Client, req *syncpb.GetLeafRequest) *syncpb.GetLeafResponse {
	t.Helper()
	resp, err := c.sender.Send(ctx, req,
		func(*syncpb.GetLeafResponse) error { return nil },
	)
	require.NoError(t, err)
	return resp
}

func TestClient_FetchLeaves(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		numKeys  int
		limit    uint16
		wantLen  int
		wantMore bool
	}{
		{
			name:    "whole_trie",
			numKeys: 50,
			limit:   MaxLeavesLimit,
			wantLen: 50,
		},
		{
			name:     "bounded_by_limit",
			numKeys:  200,
			limit:    20,
			wantLen:  20,
			wantMore: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			trieDB := synctest.NewTrieDB()
			root, keys, vals := synctest.FillTrie(t, trieDB, tt.numKeys)

			got, more, err := serve(t, ctx, trieDB).FetchLeaves(ctx, LeafRange{Root: root, Limit: tt.limit})
			require.NoError(t, err)

			require.Equal(t, keys[:tt.wantLen], got.Keys)
			require.Equal(t, vals[:tt.wantLen], got.Vals)
			require.Equal(t, tt.wantMore, more)
		})
	}
}

func TestVerifyRange(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	trieDB := synctest.NewTrieDB()
	root, _, _ := synctest.FillTrie(t, trieDB, 50)
	client := serve(t, ctx, trieDB)

	req := func(limit uint32) *syncpb.GetLeafRequest {
		return &syncpb.GetLeafRequest{RootHash: root.Bytes(), KeyLimit: limit}
	}
	partial := rawResponse(t, ctx, client, req(20))
	whole := rawResponse(t, ctx, client, req(uint32(MaxLeavesLimit)))

	tampered := &syncpb.GetLeafResponse{
		Keys:      partial.GetKeys(),
		Values:    append([][]byte{bytes.Repeat([]byte{0xff}, common.HashLength)}, partial.GetValues()[1:]...),
		ProofVals: partial.GetProofVals(),
	}

	tests := []struct {
		name     string
		resp     *syncpb.GetLeafResponse
		wantMore bool
		wantErr  error
	}{
		{
			name:     "partial_has_more",
			resp:     partial,
			wantMore: true,
		},
		{
			name: "whole_trie_has_no_more",
			resp: whole,
		},
		{
			name:    "tampered_value_fails_proof",
			resp:    tampered,
			wantErr: errInvalidRangeProof,
		},
		{
			name:    "empty_without_proof",
			resp:    &syncpb.GetLeafResponse{},
			wantErr: errEmptyLeafResponse,
		},
		{
			name:    "more_leaves_than_requested",
			resp:    &syncpb.GetLeafResponse{Keys: make([][]byte, int(MaxLeavesLimit)+1)},
			wantErr: errTooManyLeaves,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			more, err := verifyRange(LeafRange{Root: root, Limit: MaxLeavesLimit}, tt.resp)
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, tt.wantMore, more)
		})
	}
}

// tamperLeaf corrupts a served value so its range proof fails, leaving the
// client's own verification as the thing under test.
func tamperLeaf(resp *syncpb.GetLeafResponse) {
	if len(resp.GetValues()) > 0 {
		resp.Values[0] = bytes.Repeat([]byte{0xff}, common.HashLength)
	}
}

// Without verification the client would hand a tampered range to its caller.
func TestClient_RejectsTamperedRange(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	trieDB := synctest.NewTrieDB()
	root, _, _ := synctest.FillTrie(t, trieDB, 50)
	log := loggingtest.New(t, logging.Debug)

	// Corrupting as many responses as the guard allows tampers every one the
	// client sees, and cancelling ends a fetch that never converges.
	const attempts = 3
	tampering := synctest.NewMutatingResponder(newLeafResponder(t, trieDB), attempts, tamperLeaf)
	recording := synctest.NewRecordingResponder(tampering)
	net, tracker := synctest.ServeResponder(t, ctx, log, p2p.EVMLeafRequestHandlerID,
		synctest.NewCancelAfter(recording, attempts, cancel))

	got, _, err := NewClient(log, net, p2p.EVMLeafRequestHandlerID, tracker).
		FetchLeaves(ctx, LeafRange{Root: root, Limit: MaxLeavesLimit})
	require.ErrorIs(t, err, context.Canceled, "a tampered range must never be returned")
	require.Empty(t, got.Keys)
	require.Len(t, recording.Requests(), attempts, "each tampered range must be re-requested")
}

func TestClient_FetchesStorageRange(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	trieDB := synctest.NewTrieDB()
	c := newStorageCase(t, trieDB, 20)

	got, more, err := serve(t, ctx, trieDB).FetchLeaves(ctx, LeafRange{
		Root:    c.root,
		Account: c.account,
		Limit:   MaxLeavesLimit,
	})
	require.NoError(t, err)
	require.False(t, more)
	require.Equal(t, c.keys, got.Keys)
	require.Equal(t, c.vals, got.Vals)
}
