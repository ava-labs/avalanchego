// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"testing"

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
		name      string
		vertex    StatelessVertex
		shouldErr bool
	}{
		{
			name:      "zero vertex",
			vertex:    statelessVertex{innerStatelessVertex: innerStatelessVertex{}},
			shouldErr: true,
		},
		{
			name: "valid vertex",
			vertex: statelessVertex{innerStatelessVertex: innerStatelessVertex{
				Version:   0,
				ChainID:   ids.ID{},
				Height:    0,
				Epoch:     0,
				ParentIDs: []ids.ID{},
				Txs:       [][]byte{{}},
			}},
			shouldErr: false,
		},
		{
			name: "invalid vertex epoch",
			vertex: statelessVertex{innerStatelessVertex: innerStatelessVertex{
				Version:   0,
				ChainID:   ids.ID{},
				Height:    0,
				Epoch:     1,
				ParentIDs: []ids.ID{},
				Txs:       [][]byte{{}},
			}},
			shouldErr: true,
		},
		{
			name: "too many vertex parents",
			vertex: statelessVertex{innerStatelessVertex: innerStatelessVertex{
				Version:   0,
				ChainID:   ids.ID{},
				Height:    0,
				Epoch:     0,
				ParentIDs: tooManyParents,
				Txs:       [][]byte{{}},
			}},
			shouldErr: true,
		},
		{
			name: "no vertex txs",
			vertex: statelessVertex{innerStatelessVertex: innerStatelessVertex{
				Version:   0,
				ChainID:   ids.ID{},
				Height:    0,
				Epoch:     0,
				ParentIDs: []ids.ID{},
				Txs:       [][]byte{},
			}},
			shouldErr: true,
		},
		{
			name: "too many vertex txs",
			vertex: statelessVertex{innerStatelessVertex: innerStatelessVertex{
				Version:   0,
				ChainID:   ids.ID{},
				Height:    0,
				Epoch:     0,
				ParentIDs: []ids.ID{},
				Txs:       tooManyTxs,
			}},
			shouldErr: true,
		},
		{
			name: "unsorted vertex parents",
			vertex: statelessVertex{innerStatelessVertex: innerStatelessVertex{
				Version:   0,
				ChainID:   ids.ID{},
				Height:    0,
				Epoch:     0,
				ParentIDs: []ids.ID{{1}, {0}},
				Txs:       [][]byte{{}},
			}},
			shouldErr: true,
		},
		{
			name: "unsorted vertex txs",
			vertex: statelessVertex{innerStatelessVertex: innerStatelessVertex{
				Version:   0,
				ChainID:   ids.ID{},
				Height:    0,
				Epoch:     0,
				ParentIDs: []ids.ID{},
				Txs:       [][]byte{{0}, {1}}, // note that txs are sorted by their hashes
			}},
			shouldErr: true,
		},
		{
			name: "duplicate vertex parents",
			vertex: statelessVertex{innerStatelessVertex: innerStatelessVertex{
				Version:   0,
				ChainID:   ids.ID{},
				Height:    0,
				Epoch:     0,
				ParentIDs: []ids.ID{{0}, {0}},
				Txs:       [][]byte{{}},
			}},
			shouldErr: true,
		},
		{
			name: "duplicate vertex txs",
			vertex: statelessVertex{innerStatelessVertex: innerStatelessVertex{
				Version:   0,
				ChainID:   ids.ID{},
				Height:    0,
				Epoch:     0,
				ParentIDs: []ids.ID{},
				Txs:       [][]byte{{0}, {0}}, // note that txs are sorted by their hashes
			}},
			shouldErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.vertex.Verify()
			if test.shouldErr && err == nil {
				t.Fatal("expected verify to return an error but it didn't")
			} else if !test.shouldErr && err != nil {
				t.Fatalf("expected verify to pass but it returned: %s", err)
			}
		})
	}
}
