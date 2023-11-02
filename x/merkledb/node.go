// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"golang.org/x/exp/maps"
)

const HashLength = 32

// Representation of a node stored in the database.
type dbNode struct {
	value    maybe.Maybe[[]byte]
	children map[byte]child
}

type child struct {
	compressedKey Key
	id            ids.ID
	hasValue      bool
}

// node holds additional information on top of the dbNode that makes calculations easier to do
type node struct {
	dbNode
}

// Returns a new node with the given [key] and no value.
// If [parent] isn't nil, the new node is added as a child of [parent].
func newNode() *node {
	return &node{
		dbNode: dbNode{
			children: make(map[byte]child, 2),
		},
	}
}

// Parse [nodeBytes] to a node and set its key to [key].
func parseNode(nodeBytes []byte) (*node, error) {
	n := dbNode{}
	if err := codec.decodeDBNode(nodeBytes, &n); err != nil {
		return nil, err
	}
	result := &node{
		dbNode: n,
	}
	return result, nil
}

// Returns true iff this node has a value.
func (n *node) hasValue() bool {
	return !n.value.IsNothing()
}

// Returns the byte representation of this node.
func (n *node) bytes() []byte {
	return codec.encodeDBNode(&n.dbNode)
}

// Returns and caches the ID of this node.
func (n *node) calculateID(key Key, metrics merkleMetrics) ids.ID {
	metrics.HashCalculated()
	return hashing.ComputeHash256Array(codec.encodeHashValues(key, n))
}

// Set [n]'s value to [val].
func (n *node) setValue(val maybe.Maybe[[]byte]) {
	n.value = val
}

func (n *node) getValueDigest() maybe.Maybe[[]byte] {
	if n.value.IsNothing() || len(n.value.Value()) <= HashLength {
		return n.value
	}
	return maybe.Some(hashing.ComputeHash256(n.value.Value()))
}

// Adds a child to [n] without a reference to the child node.
func (n *node) setChildEntry(index byte, childEntry child) {
	n.children[index] = childEntry
}

// clone Returns a copy of [n].
// Note: value isn't cloned because it is never edited, only overwritten
// if this ever changes, value will need to be copied as well
// it is safe to clone all fields because they are only written/read while one or both of the db locks are held
func (n *node) clone() *node {
	return &node{
		dbNode: dbNode{
			value:    n.value,
			children: maps.Clone(n.children),
		},
	}
}

// Returns the ProofNode representation of this node.
func (n *node) asProofNode(key Key) ProofNode {
	pn := ProofNode{
		Key:         key,
		Children:    make(map[byte]ids.ID, len(n.children)),
		ValueOrHash: n.getValueDigest(),
	}
	for index, entry := range n.children {
		pn.Children[index] = entry.id
	}
	return pn
}
