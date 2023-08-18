// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

var errNoNewRoot = errors.New("there was no updated root in change list")

type MerkleRootGetter interface {
	// GetMerkleRoot returns the merkle root of the Trie
	GetMerkleRoot(ctx context.Context) (ids.ID, error)
}

type ProofGetter interface {
	// GetProof generates a proof of the value associated with a particular key,
	// or a proof of its absence from the trie
	GetProof(ctx context.Context, bytesPath []byte) (*Proof, error)
}

type ReadOnlyTrie interface {
	MerkleRootGetter
	ProofGetter

	// GetValue gets the value associated with the specified key
	// database.ErrNotFound if the key is not present
	GetValue(ctx context.Context, key []byte) ([]byte, error)

	// GetValues gets the values associated with the specified keys
	// database.ErrNotFound if the key is not present
	GetValues(ctx context.Context, keys [][]byte) ([][]byte, []error)

	// get the value associated with the key in path form
	// database.ErrNotFound if the key is not present
	getValue(key path, lock bool) ([]byte, error)

	// get an editable copy of the node with the given key path
	getEditableNode(key path) (*node, error)

	// GetRangeProof returns a proof of up to [maxLength] key-value pairs with
	// keys in range [start, end].
	// If [start] is Nothing, there's no lower bound on the range.
	// If [end] is Nothing, there's no upper bound on the range.
	GetRangeProof(ctx context.Context, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], maxLength int) (*RangeProof, error)

	database.Iteratee
}

type Trie interface {
	ReadOnlyTrie

	// NewView returns a new view on top of this Trie with the specified changes
	NewView(batchOps []database.BatchOp) (TrieView, error)
}

type TrieView interface {
	Trie

	// CommitToDB takes the changes of this trie and commits them down the view stack
	// until all changes in the stack commit to the database
	// Takes the DB commit lock
	CommitToDB(ctx context.Context) error

	// commits changes in the trie to its parent
	// then commits the combined changes down the stack until all changes in the stack commit to the database
	commitToDB(ctx context.Context) error

	// commits changes in the trieToCommit into the current trie
	commitChanges(ctx context.Context, trieToCommit *trieView) error
}
