// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

type MerkleRootGetter interface {
	// GetMerkleRoot returns the merkle root of the Trie
	GetMerkleRoot(ctx context.Context) (ids.ID, error)
}

type ProofGetter interface {
	// GetProof generates a proof of the value associated with a particular key,
	// or a proof of its absence from the trie
	GetProof(ctx context.Context, keyBytes []byte) (*Proof, error)
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
	getValue(key Key) ([]byte, error)

	// get an editable copy of the node with the given key path
	// hasValue indicates which db to look in (value or intermediate)
	getEditableNode(key Key, hasValue bool) (*node, error)

	// GetRangeProof returns a proof of up to [maxLength] key-value pairs with
	// keys in range [start, end].
	// If [start] is Nothing, there's no lower bound on the range.
	// If [end] is Nothing, there's no upper bound on the range.
	GetRangeProof(ctx context.Context, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], maxLength int) (*RangeProof, error)

	database.Iteratee
}

type ViewChanges struct {
	BatchOps []database.BatchOp
	MapOps   map[string]maybe.Maybe[[]byte]
	// ConsumeBytes when set to true will skip copying of bytes and assume
	// ownership of the provided bytes.
	ConsumeBytes bool
}

type Trie interface {
	ReadOnlyTrie

	// NewView returns a new view on top of this Trie where the passed changes
	// have been applied.
	NewView(
		ctx context.Context,
		changes ViewChanges,
	) (TrieView, error)
}

type TrieView interface {
	Trie

	// CommitToDB writes the changes in this view to the database.
	// Takes the DB commit lock.
	CommitToDB(ctx context.Context) error
}
