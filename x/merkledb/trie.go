// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"context"
	"fmt"
	"golang.org/x/exp/slices"

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

type ViewChanges struct {
	BatchOps []database.BatchOp
	MapOps   map[string]maybe.Maybe[[]byte]
	// ConsumeBytes when set to true will skip copying of bytes and assume
	// ownership of the provided bytes.
	ConsumeBytes bool
}

type trieInternal interface {
	// get the value associated with the key in path form
	// database.ErrNotFound if the key is not present
	getValue(key Key) ([]byte, error)

	// get an editable copy of the node with the given key path
	// hasValue indicates which db to look in (value or intermediate)
	getEditableNode(key Key, hasValue bool) (*node, error)

	// get the node associated with the key without locking
	getNode(key Key, hasValue bool) (*node, error)

	// get the sentinel node without locking
	getSentinelNode() *node

	getTokenSize() int
}

type Trie interface {
	trieInternal
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
	GetRangeProof(ctx context.Context, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], maxLength int) (*RangeProof, error)

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

func getRoot[T Trie](t T) (*node, error) {
	sentinel := t.getSentinelNode()
	if !isSentinelNodeTheRoot(sentinel) {
		// sentinel has one child, which is the root
		for index, childEntry := range sentinel.children {
			return t.getNode(
				sentinel.key.Extend(ToToken(index, t.getTokenSize()), childEntry.compressedKey),
				childEntry.hasValue)
		}
	}

	return sentinel, nil
}

// Returns the nodes along the path to [key].
// The first node is the root, and the last node is either the node with the
// given [key], if it's in the trie, or the node with the largest prefix of
// the [key] if it isn't in the trie.
// Always returns at least the root node.
func visitPathToKey[T Trie](t T, key Key, visitNode func(*node) error) error {
	var (
		// all node paths start at the sentinelNode since its nil key is a prefix of all keys
		currentNode = t.getSentinelNode()
		err         error
		tSize       = t.getTokenSize()
	)
	if err := visitNode(currentNode); err != nil {
		return err
	}
	// while the entire path hasn't been matched
	for currentNode.key.length < key.length {
		// confirm that a child exists and grab its ID before attempting to load it
		nextChildEntry, hasChild := currentNode.children[key.Token(currentNode.key.length, tSize)]

		if !hasChild || !key.iteratedHasPrefix(nextChildEntry.compressedKey, currentNode.key.length+tSize, tSize) {
			// there was no child along the path or the child that was there doesn't match the remaining path
			return nil
		}
		// grab the next node along the path
		currentNode, err = t.getNode(key.Take(currentNode.key.length+tSize+nextChildEntry.compressedKey.length), nextChildEntry.hasValue)
		if err != nil {
			return err
		}
		if err := visitNode(currentNode); err != nil {
			return err
		}
	}
	return nil
}

// Returns a proof that [bytesPath] is in or not in trie [t].
func getProof[T Trie](t T, key []byte) (*Proof, error) {
	proof := &Proof{
		Key: ToKey(key),
	}

	var closestNode *node
	if err := visitPathToKey(t, proof.Key, func(n *node) error {
		closestNode = n
		proof.Path = append(proof.Path, n.asProofNode())
		return nil
	}); err != nil {
		return nil, err
	}
	root, err := getRoot(t)
	if err != nil {
		return nil, err
	}
	// The sentinel node is always the first node in the path.
	// If the sentinel node is not the root, remove it from the proofPath.
	if root != t.getSentinelNode() {
		proof.Path = proof.Path[1:]

		// if there are no nodes in the proof path, add the root to serve as an exclusion proof
		if len(proof.Path) == 0 {
			proof.Path = []ProofNode{root.asProofNode()}
			return proof, nil
		}
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

// GetRangeProof returns a range proof for (at least part of) the key range [start, end].
// The returned proof's [KeyValues] has at most [maxLength] values.
// [maxLength] must be > 0.
func getRangeProof[T Trie](
	t T,
	start maybe.Maybe[[]byte],
	end maybe.Maybe[[]byte],
	maxLength int,
) (*RangeProof, error) {
	if start.HasValue() && end.HasValue() && bytes.Compare(start.Value(), end.Value()) == 1 {
		return nil, ErrStartAfterEnd
	}

	if maxLength <= 0 {
		return nil, fmt.Errorf("%w but was %d", ErrInvalidMaxLength, maxLength)
	}

	var result RangeProof

	result.KeyValues = make([]KeyValue, 0, initKeyValuesSize)
	it := t.NewIteratorWithStart(start.Value())
	for it.Next() && len(result.KeyValues) < maxLength && (end.IsNothing() || bytes.Compare(it.Key(), end.Value()) <= 0) {
		// clone the value to prevent editing of the values stored within the trie
		result.KeyValues = append(result.KeyValues, KeyValue{
			Key:   it.Key(),
			Value: slices.Clone(it.Value()),
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
	if len(result.KeyValues) > 0 {
		greatestKey := result.KeyValues[len(result.KeyValues)-1].Key
		endProof, err = getProof(t, greatestKey)
		if err != nil {
			return nil, err
		}
	} else if end.HasValue() {
		endProof, err = getProof(t, end.Value())
		if err != nil {
			return nil, err
		}
	}
	if endProof != nil {
		result.EndProof = endProof.Path
	}

	if start.HasValue() {
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

	if len(result.StartProof) == 0 && len(result.EndProof) == 0 && len(result.KeyValues) == 0 {
		// If the range is empty, return the root proof.
		root, err := getRoot(t)
		if err != nil {
			return nil, err
		}
		rootProof, err := getProof(t, root.key.Bytes())
		if err != nil {
			return nil, err
		}
		result.EndProof = rootProof.Path
	}
	return &result, nil
}
