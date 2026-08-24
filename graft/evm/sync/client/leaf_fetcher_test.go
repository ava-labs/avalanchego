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

func TestLeafFetcher(t *testing.T) {
	t.Parallel()
	root := common.HexToHash("0xbb")
	account := common.HexToHash("0xaa")

	tests := []struct {
		name     string
		reqType  message.LeafsRequestType
		nodeType message.NodeType
		req      evmstate.LeafRange
		resp     message.LeafsResponse
		err      error
		want     evmstate.Leaves
		wantMore bool
		wantErr  error
	}{
		{
			name:     "account_trie_subnet_evm",
			reqType:  message.SubnetEVMLeafsRequestType,
			nodeType: message.StateTrieNode,
			req:      evmstate.LeafRange{Root: root, Limit: 1024},
		},
		{
			name:     "storage_trie_coreth",
			reqType:  message.CorethLeafsRequestType,
			nodeType: message.NodeType(2),
			req:      evmstate.LeafRange{Root: root, Account: account, Start: []byte{0x01}, End: []byte{0x02}, Limit: 16},
		},
		{
			name:     "proof_never_reaches_caller",
			reqType:  message.SubnetEVMLeafsRequestType,
			nodeType: message.StateTrieNode,
			req:      evmstate.LeafRange{Root: root, Limit: 8},
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
			name:     "client_error_propagates",
			reqType:  message.SubnetEVMLeafsRequestType,
			nodeType: message.StateTrieNode,
			req:      evmstate.LeafRange{Root: root, Limit: 8},
			err:      errStub,
			wantErr:  errStub,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stub := &stubLeafClient{resp: tt.resp, err: tt.err}
			got, more, err := NewLeafFetcher(stub, tt.reqType, tt.nodeType).FetchLeaves(t.Context(), tt.req)

			require.Equal(t, tt.req.Root, stub.got.RootHash())
			require.Equal(t, tt.req.Account, stub.got.AccountHash())
			require.Equal(t, tt.req.Start, stub.got.StartKey())
			require.Equal(t, tt.req.End, stub.got.EndKey())
			require.Equal(t, tt.req.Limit, stub.got.KeyLimit())
			// The node type comes from construction, not from the range.
			require.Equal(t, tt.nodeType, stub.got.LeafType())

			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.wantMore, more)
		})
	}
}
