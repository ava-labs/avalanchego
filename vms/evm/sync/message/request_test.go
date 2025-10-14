// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"context"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

type recordingHandler struct {
	leafsArgs struct {
		ctx       context.Context
		nodeID    ids.NodeID
		requestID uint32
		req       LeafsRequest
	}

	blockArgs struct {
		ctx       context.Context
		nodeID    ids.NodeID
		requestID uint32
		req       BlockRequest
	}

	codeArgs struct {
		ctx       context.Context
		nodeID    ids.NodeID
		requestID uint32
		req       CodeRequest
	}

	leafsCalled bool
	blockCalled bool
	codeCalled  bool
}

func (r *recordingHandler) HandleLeafsRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, req LeafsRequest) ([]byte, error) {
	r.leafsCalled = true
	r.leafsArgs.ctx = ctx
	r.leafsArgs.nodeID = nodeID
	r.leafsArgs.requestID = requestID
	r.leafsArgs.req = req
	return nil, nil
}

func (r *recordingHandler) HandleBlockRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, req BlockRequest) ([]byte, error) {
	r.blockCalled = true
	r.blockArgs.ctx = ctx
	r.blockArgs.nodeID = nodeID
	r.blockArgs.requestID = requestID
	r.blockArgs.req = req
	return nil, nil
}

func (r *recordingHandler) HandleCodeRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, req CodeRequest) ([]byte, error) {
	r.codeCalled = true
	r.codeArgs.ctx = ctx
	r.codeArgs.nodeID = nodeID
	r.codeArgs.requestID = requestID
	r.codeArgs.req = req
	return nil, nil
}

func TestRequest_HandleDispatchesToCorrectHandler(t *testing.T) {
	t.Parallel()

	ctx := context.TODO()
	nodeID := ids.EmptyNodeID
	const requestID uint32 = 42

	tests := []struct {
		name  string
		build func() Request
		check func(t *testing.T, h *recordingHandler, built Request)
	}{
		{
			name: "leafs_request",
			build: func() Request {
				return LeafsRequest{
					Root:     common.Hash{1},
					Start:    make([]byte, common.HashLength),
					End:      make([]byte, common.HashLength),
					Limit:    1,
					NodeType: StateTrieNode,
				}
			},
			check: func(t *testing.T, h *recordingHandler, built Request) {
				require := require.New(t)
				require.True(h.leafsCalled)
				require.Equal(requestID, h.leafsArgs.requestID)
				require.IsType(LeafsRequest{}, built)
			},
		},
		{
			name: "block_request",
			build: func() Request {
				return BlockRequest{Hash: common.Hash{2}, Height: 3, Parents: 1}
			},
			check: func(t *testing.T, h *recordingHandler, built Request) {
				require := require.New(t)
				require.True(h.blockCalled)
				require.Equal(requestID, h.blockArgs.requestID)
				require.IsType(BlockRequest{}, built)
			},
		},
		{
			name: "code_request",
			build: func() Request {
				return CodeRequest{Hashes: []common.Hash{{3}}}
			},
			check: func(t *testing.T, h *recordingHandler, built Request) {
				require := require.New(t)
				require.True(h.codeCalled)
				require.Equal(requestID, h.codeArgs.requestID)
				require.IsType(CodeRequest{}, built)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := new(recordingHandler)
			built := tc.build()

			_, err := built.Handle(ctx, nodeID, requestID, h)
			require.NoError(t, err)

			tc.check(t, h, built)
		})
	}
}

func TestRequestToBytes_InterfaceRoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		req  Request
	}{
		{
			name: "code",
			req: CodeRequest{
				Hashes: []common.Hash{{1}},
			},
		},
		{
			name: "leafs",
			req: LeafsRequest{
				Root:     common.Hash{2},
				Start:    make([]byte, common.HashLength),
				End:      make([]byte, common.HashLength),
				Limit:    1,
				NodeType: StateTrieNode,
			},
		},
		{
			name: "block",
			req: BlockRequest{
				Hash:    common.Hash{3},
				Height:  4,
				Parents: 1,
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, err := RequestToBytes(Codec, c.req)
			require.NoError(t, err)

			var out Request
			_, err = Codec.Unmarshal(b, &out)
			require.NoError(t, err)
			require.IsType(t, c.req, out)
		})
	}
}
