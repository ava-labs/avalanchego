// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

// Verifies each per-RPC alias returns the right typed response.
func TestClients_Send(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()

	tests := []struct {
		name     string
		request  proto.Message
		wantResp proto.Message
	}{
		{
			name: "LeafsClient",
			request: &syncpb.GetLeafsRequest{
				RootHash: []byte{0xab},
				KeyLimit: 1024,
				NodeType: syncpb.NodeType_NODE_TYPE_STATE_TRIE,
			},
			wantResp: &syncpb.LeafsResponse{
				Keys:      [][]byte{{1, 2, 3}},
				Values:    [][]byte{{4, 5, 6}},
				ProofVals: [][]byte{{7, 8}},
			},
		},
		{
			name:     "CodeClient",
			request:  &syncpb.GetCodeRequest{Hashes: [][]byte{{1}, {2}}},
			wantResp: &syncpb.CodeResponse{Data: [][]byte{{0xde, 0xad}, {0xbe, 0xef}}},
		},
		{
			name:     "BlockClient",
			request:  &syncpb.GetBlockRequest{Hash: []byte{1}, Height: 100, Parents: 5},
			wantResp: &syncpb.BlockResponse{Blocks: [][]byte{{0x01}, {0x02}, {0x03}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			wantBytes, err := proto.Marshal(tt.wantResp)
			require.NoError(t, err)

			handler := p2p.TestHandler{
				AppRequestF: func(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *common.AppError) {
					return wantBytes, nil
				},
			}
			client := p2ptest.NewSelfClient(t, ctx, nodeID, handler)
			peers := newTestPeerTracker(t, nodeID)

			var (
				got       proto.Message
				gotNodeID ids.NodeID
			)
			switch req := tt.request.(type) {
			case *syncpb.GetLeafsRequest:
				resp := &syncpb.LeafsResponse{}
				n, err := (&LeafsClient{client: client, peers: peers}).Send(ctx, req, resp)
				require.NoError(t, err)
				got, gotNodeID = resp, n
			case *syncpb.GetCodeRequest:
				resp := &syncpb.CodeResponse{}
				n, err := (&CodeClient{client: client, peers: peers}).Send(ctx, req, resp)
				require.NoError(t, err)
				got, gotNodeID = resp, n
			case *syncpb.GetBlockRequest:
				resp := &syncpb.BlockResponse{}
				n, err := (&BlockClient{client: client, peers: peers}).Send(ctx, req, resp)
				require.NoError(t, err)
				got, gotNodeID = resp, n
			default:
				t.Fatalf("unhandled request type %T", req)
			}

			require.Equal(t, nodeID, gotNodeID)
			require.Empty(t, cmp.Diff(tt.wantResp, got, protocmp.Transform()))
		})
	}
}

