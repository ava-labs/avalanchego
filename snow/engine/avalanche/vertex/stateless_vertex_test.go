// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestVertexVerify(t *testing.T) {
	tooManyParents := make([]ids.ID, maxNumParents+1)
	for i := range tooManyParents {
		tooManyParents[i][0] = byte(i)
	}
	tooManyTxs := make([][]byte, maxTxsPerVtx+1)
	for i := range tooManyTxs {
		tooManyTxs[i] = []byte{byte(i)}
	}
	tooManyRestrictions := make([]ids.ID, maxTxsPerVtx+1)
	for i := range tooManyRestrictions {
		tooManyRestrictions[i][0] = byte(i)
	}

	tests := []struct {
		name        string
		vertex      StatelessVertex
		expectedErr error
	}{
		{
			name:        "zero vertex",
			vertex:      statelessVertex{innerStatelessVertex: innerStatelessVertex{}},
			expectedErr: errNoOperations,
		},
		{
			name: "valid vertex",
			vertex: statelessVertex{innerStatelessVertex: innerStatelessVertex{
				Version:   0,
				ChainID:   ids.Empty,
				Height:    0,
				Epoch:     0,
				ParentIDs: []ids.ID{},
				Txs:       [][]byte{{}},
			}},
			expectedErr: nil,
		},
		{
			name: "invalid vertex epoch",
			vertex: statelessVertex{innerStatelessVertex: innerStatelessVertex{
				Version:   0,
				ChainID:   ids.Empty,
				Height:    0,
				Epoch:     1,
				ParentIDs: []ids.ID{},
				Txs:       [][]byte{{}},
			}},
			expectedErr: errBadEpoch,
		},
		{
			name: "too many vertex parents",
			vertex: statelessVertex{innerStatelessVertex: innerStatelessVertex{
				Version:   0,
				ChainID:   ids.Empty,
				Height:    0,
				Epoch:     0,
				ParentIDs: tooManyParents,
				Txs:       [][]byte{{}},
			}},
			expectedErr: errTooManyParentIDs,
		},
		{
			name: "no vertex txs",
			vertex: statelessVertex{innerStatelessVertex: innerStatelessVertex{
				Version:   0,
				ChainID:   ids.Empty,
				Height:    0,
				Epoch:     0,
				ParentIDs: []ids.ID{},
				Txs:       [][]byte{},
			}},
			expectedErr: errNoOperations,
		},
		{
			name: "too many vertex txs",
			vertex: statelessVertex{innerStatelessVertex: innerStatelessVertex{
				Version:   0,
				ChainID:   ids.Empty,
				Height:    0,
				Epoch:     0,
				ParentIDs: []ids.ID{},
				Txs:       tooManyTxs,
			}},
			expectedErr: errTooManyTxs,
		},
		{
			name: "unsorted vertex parents",
			vertex: statelessVertex{innerStatelessVertex: innerStatelessVertex{
				Version:   0,
				ChainID:   ids.Empty,
				Height:    0,
				Epoch:     0,
				ParentIDs: []ids.ID{{1}, {0}},
				Txs:       [][]byte{{}},
			}},
			expectedErr: errInvalidParents,
		},
		{
			name: "unsorted vertex txs",
			vertex: statelessVertex{innerStatelessVertex: innerStatelessVertex{
				Version:   0,
				ChainID:   ids.Empty,
				Height:    0,
				Epoch:     0,
				ParentIDs: []ids.ID{},
				Txs:       [][]byte{{0}, {1}}, // note that txs are sorted by their hashes
			}},
			expectedErr: errInvalidTxs,
		},
		{
			name: "duplicate vertex parents",
			vertex: statelessVertex{innerStatelessVertex: innerStatelessVertex{
				Version:   0,
				ChainID:   ids.Empty,
				Height:    0,
				Epoch:     0,
				ParentIDs: []ids.ID{{0}, {0}},
				Txs:       [][]byte{{}},
			}},
			expectedErr: errInvalidParents,
		},
		{
			name: "duplicate vertex txs",
			vertex: statelessVertex{innerStatelessVertex: innerStatelessVertex{
				Version:   0,
				ChainID:   ids.Empty,
				Height:    0,
				Epoch:     0,
				ParentIDs: []ids.ID{},
				Txs:       [][]byte{{0}, {0}}, // note that txs are sorted by their hashes
			}},
			expectedErr: errInvalidTxs,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.vertex.Verify()
			require.ErrorIs(t, err, test.expectedErr)
		})
	}
}
