// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hashdb

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

func TestGetLeafs_RetriesUntilValid(t *testing.T) {
	req, want := validLeafRequest(t)
	goodResp := synctest.MustMarshal(t, want)

	// A response with more leaves than the request's KeyLimit.
	tooManyLeaves := synctest.MustMarshal(t, &syncpb.GetLeafResponse{
		Keys: make([][]byte, int(req.KeyLimit())+1),
	})

	// A response whose values were tampered with so the range proof fails.
	corrupt := proto.Clone(want).(*syncpb.GetLeafResponse)
	corrupt.Values[0] = bytes.Repeat([]byte{0xff}, common.HashLength)
	badProof := synctest.MustMarshal(t, corrupt)

	tests := []struct {
		name    string
		badResp []byte
	}{
		{
			name:    "too many leaves is retried",
			badResp: tooManyLeaves,
		},
		{
			name:    "empty response without proof is retried",
			badResp: synctest.MustMarshal(t, &syncpb.GetLeafResponse{}),
		},
		{
			name:    "invalid range proof is retried",
			badResp: badProof,
		},
		{
			name:    "network error is retried",
			badResp: nil, // handler returns an AppError
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// responses served in order: bad first, then good.
			h := synctest.NewScriptedHandler([][]byte{tt.badResp, goodResp})
			g := newTestGetter(t, h)

			leafs, err := g.GetLeafs(t.Context(), req)
			require.NoError(t, err)
			require.Equal(t, want.Keys, leafs.Keys)
			require.Equal(t, want.Values, leafs.Vals)
			require.Equal(t, 2, h.Calls())
		})
	}
}

// TestGetLeafs_ContextCancelled asserts that a cancelled context returns the
// context error early, before any request is sent.
func TestGetLeafs_ContextCancelled(t *testing.T) {
	req, want := validLeafRequest(t)

	// A valid response is available; the cancelled context must short-circuit
	// before the handler is ever reached.
	h := synctest.NewScriptedHandler([][]byte{synctest.MustMarshal(t, want)})
	g := newTestGetter(t, h)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := g.GetLeafs(ctx, req)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, h.Calls())
}

// validLeafRequest builds a filled trie and returns a partial-range request
// (so the response carries a proof) together with the valid response the real
// responder produces for it.
func validLeafRequest(t *testing.T) (message.LeafsRequest, *syncpb.GetLeafResponse) {
	t.Helper()
	trieDB := synctest.NewTrieDB()
	root, keys, _ := synctest.FillTrie(t, trieDB, 20)

	limit := uint16(len(keys) / 2) // partial range -> proof present

	resp, err := NewResponder(trieDB, common.HashLength, nil).
		Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{
			RootHash: root.Bytes(),
			KeyLimit: uint32(limit),
		})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.ProofVals)

	req, err := message.NewLeafsRequest(leafsRequestType, root, common.Hash{}, nil, nil, limit, message.StateTrieNode)
	require.NoError(t, err)
	return req, resp
}

// newTestGetter wires a getter whose client talks to a single in-memory peer
// serving h at the leaf-request handler ID.
func newTestGetter(t *testing.T, h p2p.Handler) *Getter {
	t.Helper()
	net, tracker := synctest.NewLoopbackNetwork(t, p2p.EVMLeafRequestHandlerID, h)
	return &Getter{
		client: NewClient(net, tracker, p2p.EVMLeafRequestHandlerID),
		log:    loggingtest.New(t, logging.Debug),
	}
}
