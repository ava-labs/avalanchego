// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"context"
	"fmt"
	"slices"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

type ViewChanges struct {
	BatchOps []database.BatchOp
	MapOps   map[string]maybe.Maybe[[]byte]
	// ConsumeBytes when set to true will skip copying of bytes and assume
	// ownership of the provided bytes.
	ConsumeBytes bool
}

type MerkleRootGetter interface {
	// GetMerkleRoot returns the merkle root of the trie.
	// Returns ids.Empty if the trie is empty.
	GetMerkleRoot(ctx context.Context) (ids.ID, error)
}

type ProofGetter interface {
	// GetProof generates a proof of the value associated with a particular key,
	// or a proof of its absence from the trie
	// Returns ErrEmptyProof if the trie is empty.
	GetProof(ctx context.Context, keyBytes []byte) (*Proof, error)
}

type trieInternals interface {
	// get the value associated with the key in path form
	// database.ErrNotFound if the key is not present
	getValue(key Key) ([]byte, error)

	// get an editable copy of the node with the given key path
	// hasValue indicates which db to look in (value or intermediate)
	getEditableNode(key Key, hasValue bool) (*node, error)

	// get the node associated with the key without locking
	getNode(key Key, hasValue bool) (*node, error)

	// If this trie is non-empty, returns the root node.
	// Must be copied before modification.
	// Otherwise returns Nothing.
	getRoot() maybe.Maybe[*node]

	getTokenSize() int
}

type Trie interface {
	trieInternals
	MerkleRootGetter
	ProofGetter
	database.Iteratee

	// GetValue gets the value associated with the specified key
	// database.ErrNotFound if the key is not present
	GetValue(ctx context.Context, key []byte) ([]byte, error)

	// GetValues gets the values associated with the specified keys
	// database.ErrNotFound if the key is not present
	GetValues(ctx context.Context, keys [][]byte) ([][]byte, []error)

	// GetRangeProof returns a proof of up to [maxLength] key-value pairs with
	// keys in range [start, end].
	// If [start] is Nothing, there's no lower bound on the range.
	// If [end] is Nothing, there's no upper bound on the range.
	// Returns ErrEmptyProof if the trie is empty.
	GetRangeProof(ctx context.Context, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], maxLength int) (*RangeProof, error)

	// NewView returns a new view on top of this Trie where the passed changes
	// have been applied.
	NewView(
		ctx context.Context,
		changes ViewChanges,
	) (View, error)
}

type View interface {
	Trie

	// CommitToDB writes the changes in this view to the database.
	// Takes the DB commit lock.
	CommitToDB(ctx context.Context) error
}

// Calls [visitNode] on the nodes along the path to [key].
// The first node is the root, and the last node is either the node with the
// given [key], if it's in the trie, or the node with the largest prefix of
// the [key] if it isn't in the trie.
// Assumes [t] doesn't change while this function is running.
func visitPathToKey(t Trie, key Key, visitNode func(*node) error) error {
	maybeRoot := t.getRoot()
	if maybeRoot.IsNothing() {
		return nil
	}
	root := maybeRoot.Value()
	if !key.HasPrefix(root.key) {
		return nil
	}
	var (
		// all node paths start at the root
		currentNode = root
		tokenSize   = t.getTokenSize()
		err         error
	)
	if err := visitNode(currentNode); err != nil {
		return err
	}
	// while the entire path hasn't been matched
	for currentNode.key.length < key.length {
		// confirm that a child exists and grab its ID before attempting to load it
		nextChildEntry, hasChild := currentNode.children[key.Token(currentNode.key.length, tokenSize)]

		if !hasChild || !key.iteratedHasPrefix(nextChildEntry.compressedKey, currentNode.key.length+tokenSize, tokenSize) {
			// there was no child along the path or the child that was there doesn't match the remaining path
			return nil
		}
		// grab the next node along the path
		currentNode, err = t.getNode(key.Take(currentNode.key.length+tokenSize+nextChildEntry.compressedKey.length), nextChildEntry.hasValue)
		if err != nil {
			return err
		}
		if err := visitNode(currentNode); err != nil {
			return err
		}
	}
	return nil
}

// Returns a proof that [key] is in or not in trie [t].
// Assumes [t] doesn't change while this function is running.
func getProof(t Trie, key []byte) (*Proof, error) {
	root := t.getRoot()
	if root.IsNothing() {
		return nil, ErrEmptyProof
	}

	proof := &Proof{
		Key: ToKey(key),
	}

	var closestNode *node
	if err := visitPathToKey(t, proof.Key, func(n *node) error {
		closestNode = n
		// From root --> node from left --> right.
		proof.Path = append(proof.Path, n.asProofNode())
		return nil
	}); err != nil {
		return nil, err
	}

	if len(proof.Path) == 0 {
		// No key in [t] is a prefix of [key].
		// The root alone proves that [key] isn't in [t].
		proof.Path = append(proof.Path, root.Value().asProofNode())
		return proof, nil
	}

	if closestNode.key == proof.Key {
		// There is a node with the given [key].
		proof.Value = maybe.Bind(closestNode.value, slices.Clone[[]byte])
		return proof, nil
	}

	// There is no node with the given [key].
	// If there is a child at the index where the node would be
	// if it existed, include that child in the proof.
	nextIndex := proof.Key.Token(closestNode.key.length, t.getTokenSize())
	child, ok := closestNode.children[nextIndex]
	if !ok {
		return proof, nil
	}

	childNode, err := t.getNode(
		closestNode.key.Extend(ToToken(nextIndex, t.getTokenSize()), child.compressedKey),
		child.hasValue,
	)
	if err != nil {
		return nil, err
	}
	proof.Path = append(proof.Path, childNode.asProofNode())
	return proof, nil
}

// getRangeProof returns a range proof for (at least part of) the key range [start, end].
// The returned proof's [KeyValues] has at most [maxLength] values.
// [maxLength] must be > 0.
// Assumes [t] doesn't change while this function is running.
func getRangeProof(
	t Trie,
	start maybe.Maybe[[]byte],
	end maybe.Maybe[[]byte],
	maxLength int,
) (*RangeProof, error) {
	switch {
	case start.HasValue() && end.HasValue() && bytes.Compare(start.Value(), end.Value()) == 1:
		return nil, ErrStartAfterEnd
	case maxLength <= 0:
		return nil, fmt.Errorf("%w but was %d", ErrInvalidMaxLength, maxLength)
	case t.getRoot().IsNothing():
		return nil, ErrEmptyProof
	}

	result := RangeProof{
		KeyChanges: make([]KeyChange, 0, initKeyValuesSize),
	}
	it := t.NewIteratorWithStart(start.Value())
	for it.Next() && len(result.KeyChanges) < maxLength && (end.IsNothing() || bytes.Compare(it.Key(), end.Value()) <= 0) {
		// clone the value to prevent editing of the values stored within the trie
		result.KeyChanges = append(result.KeyChanges, KeyChange{
			Key:   it.Key(),
			Value: maybe.Some(slices.Clone(it.Value())),
		})
	}
	it.Release()
	if err := it.Error(); err != nil {
		return nil, err
	}

	// This proof may not contain all key-value pairs in [start, end] due to size limitations.
	// The end proof we provide should be for the last key-value pair in the proof, not for
	// the last key-value pair requested, which may not be in this proof.
	var (
		endProof *Proof
		err      error
	)
	if len(result.KeyChanges) > 0 {
		// [endProof] => inclusion proof for the largest key
		greatestKey := result.KeyChanges[len(result.KeyChanges)-1].Key
		endProof, err = getProof(t, greatestKey)
		if err != nil {
			return nil, err
		}
	} else if end.HasValue() {
		// [endProof] => exclusion proof for the [end] key
		endProof, err = getProof(t, end.Value())
		if err != nil {
			return nil, err
		}
	}
	if endProof != nil {
		result.EndProof = endProof.Path
	}

	if start.HasValue() {
		// [startProof] => inclusion/exclusion proof for [start] key
		startProof, err := getProof(t, start.Value())
		if err != nil {
			return nil, err
		}
		result.StartProof = startProof.Path

		// strip out any common nodes to reduce proof size
		i := 0
		for ; i < len(result.StartProof) &&
			i < len(result.EndProof) &&
			result.StartProof[i].Key == result.EndProof[i].Key; i++ {
		}
		result.StartProof = result.StartProof[i:]
	}

	return &result, nil
}
