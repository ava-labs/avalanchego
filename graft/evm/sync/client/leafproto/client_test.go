// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package leafproto

import (
	"bytes"
	"context"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/evmstate"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

const requestLimit = 1024

// serve registers the leaf handler for trieDB and returns a bound client.
func serve(t *testing.T, ctx context.Context, trieDB *triedb.Database) *Client {
	t.Helper()
	log := loggingtest.New(t, logging.Debug)
	net, tracker := synctest.NewSelfNetwork(t, ctx, ids.GenerateTestNodeID())
	require.NoError(t, evmstate.RegisterHandler(log, net, p2p.EVMLeafRequestHandlerID, trieDB, common.HashLength))
	return NewClient(log, evmstate.NewClient(net, p2p.EVMLeafRequestHandlerID, tracker))
}

// rawResponse fetches a range at the wire level, so a test can build cases from
// proofs the handler really produced.
func rawResponse(t *testing.T, ctx context.Context, c *Client, root common.Hash, limit uint16) *syncpb.GetLeafResponse {
	t.Helper()
	resp := &syncpb.GetLeafResponse{}
	_, err := c.sender.Send(ctx, &syncpb.GetLeafRequest{
		RootHash: root.Bytes(),
		KeyLimit: uint32(limit),
	}, resp)
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
		{name: "whole_trie", numKeys: 50, limit: requestLimit, wantLen: 50},
		{name: "bounded_by_limit", numKeys: 200, limit: 20, wantLen: 20, wantMore: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			trieDB := synctest.NewTrieDB()
			root, keys, vals := synctest.FillTrie(t, trieDB, tt.numKeys)

			got, more, err := serve(t, ctx, trieDB).FetchLeaves(ctx, types.LeafRange{Root: root, Limit: tt.limit})
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

	partial := rawResponse(t, ctx, client, root, 20)
	whole := rawResponse(t, ctx, client, root, requestLimit)

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
		{name: "partial_has_more", resp: partial, wantMore: true},
		{name: "whole_trie_has_no_more", resp: whole},
		{name: "tampered_value_fails_proof", resp: tampered, wantErr: errInvalidRangeProof},
		{name: "empty_without_proof", resp: &syncpb.GetLeafResponse{}, wantErr: errEmptyLeafResponse},
		{
			name:    "more_leaves_than_requested",
			resp:    &syncpb.GetLeafResponse{Keys: make([][]byte, requestLimit+1)},
			wantErr: errTooManyLeaves,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			more, err := verifyRange(types.LeafRange{Root: root, Limit: requestLimit}, tt.resp)
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, tt.wantMore, more)
		})
	}
}
