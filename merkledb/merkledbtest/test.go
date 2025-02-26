// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledbtest

import (
	"testing"
	"github.com/stretchr/testify/require"
	"iter"
	"github.com/ava-labs/avalanchego/merkledb"
)

func TestSeq2() iter.Seq2[string, func(t *testing.T, db merkledb.Database)] {
	tests := []struct{
		name string
		fn func(t *testing.T, db merkledb.Database)
	} {
		{
			name: "test_propose",
			fn: TestPropose,
		},
		{
			name: "test_get_from_proposal",
			fn:   TestGetFromProposal,
		},
	}

	return func(
		yield func(
		name string,
		fn func(*testing.T, merkledb.Database),
	) bool,
	) {
		for _, tt := range tests {
			if !yield(tt.name, tt.fn) {
				return
			}
		}
	}
}

func TestPropose(t *testing.T, db merkledb.Database) {
	require := require.New(t)

	key := []byte("foo")
	wantVal := []byte("bar")
	cs := merkledb.Changes{}
	cs.Append(key, wantVal)

	gotVal, err := db.Get(key)
	require.Nil(gotVal)
	require.ErrorIs(err, merkledb.ErrNotFound)

	proposal, err := db.Propose(cs)
	require.NoError(err)

	require.NoError(proposal.Commit())

	gotVal, err = db.Get(key)
	require.Equal(wantVal, gotVal)
	require.NoError(err)
}

func TestGetFromProposal(t *testing.T, db merkledb.Database) {
	require := require.New(t)

	key := []byte("foo")
	wantVal := []byte("bar")
	cs := merkledb.Changes{}
	cs.Append(key, wantVal)

	proposal, err := db.Propose(cs)
	require.NoError(err)

	require.NoError(proposal.Commit())

	proposal, err = db.Propose(merkledb.Changes{})
	require.NoError(err)

	gotVal, err := proposal.Get(key)
	require.NoError(err)
	require.Equal(wantVal, gotVal)
}
