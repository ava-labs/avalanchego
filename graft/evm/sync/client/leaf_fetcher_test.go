// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package client

import (
	"context"
	"errors"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/vms/evm/sync/evmstate"
)

var errStub = errors.New("network down")

// stubLeafClient records the request it was handed and answers with canned data.
type stubLeafClient struct {
	got  message.LeafsRequest
	resp message.LeafsResponse
	err  error
}

func (s *stubLeafClient) GetLeafs(_ context.Context, req message.LeafsRequest) (message.LeafsResponse, error) {
	s.got = req
	return s.resp, s.err
}

// The stub answers with nothing, so only the request the range becomes is checked.
// The node type and the concrete request type both come from construction.
func TestLeafFetcher_TranslatesRange(t *testing.T) {
	t.Parallel()
	root := common.HexToHash("0xbb")
	account := common.HexToHash("0xaa")

	tests := []struct {
		name     string
		reqType  message.LeafsRequestType
		nodeType message.NodeType
		req      evmstate.LeafRange
		want     message.LeafsRequest
	}{
		{
			name:     "account_trie_subnet_evm",
			reqType:  message.SubnetEVMLeafsRequestType,
			nodeType: message.StateTrieNode,
			req:      evmstate.LeafRange{Root: root, Limit: 1024},
			want: message.SubnetEVMLeafsRequest{
				Root: root, Limit: 1024, NodeType: message.StateTrieNode,
			},
		},
		{
			name:     "storage_trie_coreth",
			reqType:  message.CorethLeafsRequestType,
			nodeType: message.NodeType(2),
			req:      evmstate.LeafRange{Root: root, Account: account, Start: []byte{0x01}, End: []byte{0x02}, Limit: 16},
			want: message.CorethLeafsRequest{
				Root: root, Account: account, Start: []byte{0x01}, End: []byte{0x02},
				Limit: 16, NodeType: message.NodeType(2),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stub := &stubLeafClient{}
			_, _, err := NewLeafFetcher(stub, tt.reqType, tt.nodeType).FetchLeaves(t.Context(), tt.req)
			require.NoError(t, err)
			require.Equal(t, tt.want, stub.got)
		})
	}
}

func TestLeafFetcher_UnpacksResponse(t *testing.T) {
	t.Parallel()
	root := common.HexToHash("0xbb")

	tests := []struct {
		name     string
		resp     message.LeafsResponse
		err      error
		want     evmstate.Leaves
		wantMore bool
	}{
		{
			// The proof is verified by the client, so it never reaches the caller.
			name: "proof_never_reaches_caller",
			resp: message.LeafsResponse{
				Keys:      [][]byte{{0x01}, {0x02}},
				Vals:      [][]byte{{0x0a}, {0x0b}},
				More:      true,
				ProofVals: [][]byte{{0xff}},
			},
			want:     evmstate.Leaves{Keys: [][]byte{{0x01}, {0x02}}, Vals: [][]byte{{0x0a}, {0x0b}}},
			wantMore: true,
		},
		{
			name: "client_error_propagates",
			err:  errStub,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stub := &stubLeafClient{resp: tt.resp, err: tt.err}
			got, more, err := NewLeafFetcher(stub, message.SubnetEVMLeafsRequestType, message.StateTrieNode).
				FetchLeaves(t.Context(), evmstate.LeafRange{Root: root, Limit: 8})

			require.ErrorIs(t, err, tt.err)
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.wantMore, more)
		})
	}
}
