// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"context"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils"
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
	return NewClient(log, net, p2p.EVMLeafRequestHandlerID, common.HashLength, tracker)
}

func TestClient_FetchLeaves(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		numKeys int
		// startAt indexes the first leaf wanted, 0 leaves the start key unset.
		startAt  int
		account  *common.Hash
		limit    uint16
		wantLen  int
		wantMore bool
	}{
		{
			name:    "whole_trie",
			numKeys: 50,
			limit:   maxLimit,
			wantLen: 50,
		},
		{
			name:     "bounded_by_limit",
			numKeys:  200,
			limit:    20,
			wantLen:  20,
			wantMore: true,
		},
		{
			name:    "from_start_key",
			numKeys: 50,
			startAt: 10,
			limit:   maxLimit,
			wantLen: 40,
		},
		{
			name:    "storage_trie",
			numKeys: 50,
			account: utils.PointerTo(common.HexToHash("0xa11ce")),
			limit:   maxLimit,
			wantLen: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			trieDB := synctest.NewTrieDB()
			root, keys, vals := synctest.FillTrie(t, trieDB, tt.numKeys)

			var start []byte
			if tt.startAt > 0 {
				start = keys[tt.startAt]
			}

			client := serve(t, ctx, trieDB)
			got, more, err := client.FetchLeaves(ctx, LeafRange{
				Root:    root,
				Account: tt.account,
				Start:   start,
				Limit:   tt.limit,
			})
			require.NoError(t, err)

			to := tt.startAt + tt.wantLen
			require.Equal(t, keys[tt.startAt:to], got.Keys)
			require.Equal(t, vals[tt.startAt:to], got.Vals)
			require.Equal(t, tt.wantMore, more)
		})
	}
}

// Without verification the client would hand a corrupted range to its caller.
func TestClient_RetriesInvalidResponses(t *testing.T) {
	t.Parallel()

	trieDB := synctest.NewTrieDB()
	root, keys, vals := synctest.FillTrie(t, trieDB, 50)

	tests := []struct {
		name   string
		tamper func(*syncpb.GetLeafResponse)
	}{
		{
			name: "incorrect_key",
			tamper: func(resp *syncpb.GetLeafResponse) {
				resp.Keys[0][0]++
			},
		},
		{
			name: "incorrect_value",
			tamper: func(resp *syncpb.GetLeafResponse) {
				resp.Values[0][0]++
			},
		},
		{
			name: "missing_slot",
			tamper: func(resp *syncpb.GetLeafResponse) {
				resp.Keys = resp.Keys[1:]
				resp.Values = resp.Values[1:]
			},
		},
		{
			name: "trailing_incorrect_slot",
			tamper: func(resp *syncpb.GetLeafResponse) {
				resp.Keys = append(resp.Keys, slices.Repeat([]byte{0xff}, common.HashLength))
				resp.Values = append(resp.Values, resp.Values[0])
			},
		},
		{
			name: "missing_proof",
			tamper: func(resp *syncpb.GetLeafResponse) {
				resp.ProofVals = nil
			},
		},
		{
			name: "empty_response",
			tamper: func(resp *syncpb.GetLeafResponse) {
				*resp = syncpb.GetLeafResponse{}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			const limit = 20

			log := loggingtest.New(t, logging.Debug)

			tampering := synctest.NewMutatingResponder(newLeafResponder(t, trieDB), 1, tt.tamper)
			recording := synctest.NewRecordingResponder(tampering)
			net, tracker := synctest.ServeResponder(t, ctx, log, p2p.EVMLeafRequestHandlerID, recording)
			client := NewClient(log, net, p2p.EVMLeafRequestHandlerID, common.HashLength, tracker)

			got, more, err := client.FetchLeaves(ctx, LeafRange{Root: root, Limit: limit})
			require.NoError(t, err)

			require.Equal(t, keys[:limit], got.Keys)
			require.Equal(t, vals[:limit], got.Vals)
			require.True(t, more)
			require.Len(t, recording.Requests(), 2, "the invalid response must be re-requested")
		})
	}
}

// Incorrect responses should be retried until either a correct response is
// received or the context is cancelled. This test verifies that the context
// cancellation gracefully exists.
func TestClient_CancelEndsRetries(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	trieDB := synctest.NewTrieDB()
	root, _, _ := synctest.FillTrie(t, trieDB, 50)
	log := loggingtest.New(t, logging.Debug)

	const attempts = 3
	responder := newLeafResponder(t, trieDB)
	tampering := synctest.NewMutatingResponder(responder, attempts, func(resp *syncpb.GetLeafResponse) {
		*resp = syncpb.GetLeafResponse{} // Empty responses are invalid.
	})
	net, tracker := synctest.ServeResponder(
		t,
		ctx,
		log,
		p2p.EVMLeafRequestHandlerID,
		synctest.NewCancelAfter(tampering, attempts, cancel),
	)
	client := NewClient(log, net, p2p.EVMLeafRequestHandlerID, common.HashLength, tracker)

	got, _, err := client.FetchLeaves(ctx, LeafRange{Root: root, Limit: maxLimit})
	require.ErrorIs(t, err, context.Canceled, "a tampered range must never be returned")
	require.Zero(t, got)
}
