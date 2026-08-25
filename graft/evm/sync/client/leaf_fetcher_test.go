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

type stubLeafClient struct {
	got  message.LeafsRequest
	resp message.LeafsResponse
	err  error
}

func (s *stubLeafClient) GetLeafs(_ context.Context, req message.LeafsRequest) (message.LeafsResponse, error) {
	s.got = req
	return s.resp, s.err
}

// Each case sets both directions: the request a range becomes, and the leaves a
// response becomes. Request type and node type come from construction.
func TestLeafFetcher_FetchLeaves(t *testing.T) {
	t.Parallel()
	root := common.HexToHash("0xbb")
	account := common.HexToHash("0xaa")
	errStub := errors.New("network down")

	tests := []struct {
		name     string
		reqType  message.LeafsRequestType
		nodeType message.NodeType
		req      evmstate.LeafRange
		resp     message.LeafsResponse
		err      error
		wantReq  message.LeafsRequest
		want     evmstate.Leaves
	}{
		{
			name:     "account_trie_over_subnet_evm",
			reqType:  message.SubnetEVMLeafsRequestType,
			nodeType: message.StateTrieNode,
			req: evmstate.LeafRange{
				Root:  root,
				Limit: 1024,
			},
			resp: message.LeafsResponse{
				Keys: [][]byte{{0x01}},
				Vals: [][]byte{{0x0a}},
			},
			wantReq: message.SubnetEVMLeafsRequest{
				Root:     root,
				Limit:    1024,
				NodeType: message.StateTrieNode,
			},
			want: evmstate.Leaves{
				Keys: [][]byte{{0x01}},
				Vals: [][]byte{{0x0a}},
			},
		},
		{
			name:     "storage_trie_over_coreth",
			reqType:  message.CorethLeafsRequestType,
			nodeType: message.NodeType(2),
			req: evmstate.LeafRange{
				Root:    root,
				Account: account,
				Start:   []byte{0x01},
				End:     []byte{0x02},
				Limit:   16,
			},
			resp: message.LeafsResponse{
				Keys: [][]byte{{0x03}},
				Vals: [][]byte{{0x0c}},
			},
			wantReq: message.CorethLeafsRequest{
				Root:     root,
				Account:  account,
				Start:    []byte{0x01},
				End:      []byte{0x02},
				Limit:    16,
				NodeType: message.NodeType(2),
			},
			want: evmstate.Leaves{
				Keys: [][]byte{{0x03}},
				Vals: [][]byte{{0x0c}},
			},
		},
		{
			// The client verifies the proof, so it never reaches the caller.
			name:     "proof_is_stripped_and_more_is_carried",
			reqType:  message.SubnetEVMLeafsRequestType,
			nodeType: message.StateTrieNode,
			req:      evmstate.LeafRange{Root: root, Limit: 8},
			resp: message.LeafsResponse{
				Keys:      [][]byte{{0x01}, {0x02}},
				Vals:      [][]byte{{0x0a}, {0x0b}},
				More:      true,
				ProofVals: [][]byte{{0xff}},
			},
			wantReq: message.SubnetEVMLeafsRequest{
				Root:     root,
				Limit:    8,
				NodeType: message.StateTrieNode,
			},
			want: evmstate.Leaves{
				Keys: [][]byte{{0x01}, {0x02}},
				Vals: [][]byte{{0x0a}, {0x0b}},
			},
		},
		{
			name:     "client_error_propagates",
			reqType:  message.SubnetEVMLeafsRequestType,
			nodeType: message.StateTrieNode,
			req: evmstate.LeafRange{
				Root:  root,
				Limit: 8,
			},
			err: errStub,
			wantReq: message.SubnetEVMLeafsRequest{
				Root:     root,
				Limit:    8,
				NodeType: message.StateTrieNode,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stub := &stubLeafClient{resp: tt.resp, err: tt.err}

			got, more, err := NewLeafFetcher(stub, tt.reqType, tt.nodeType).
				FetchLeaves(t.Context(), tt.req)

			require.ErrorIs(t, err, tt.err)
			require.Equal(t, tt.wantReq, stub.got)
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.resp.More, more)
		})
	}
}
