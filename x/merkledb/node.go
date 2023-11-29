// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"golang.org/x/exp/slices"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

const HashLength = 32

// Representation of a node stored in the database.
type dbNode struct {
	value    maybe.Maybe[[]byte]
	children map[byte]*child
}

type child struct {
	compressedKey Key
	id            ids.ID
	hasValue      bool
}

// node holds additional information on top of the dbNode that makes calculations easier to do
type node struct {
	dbNode
	key         Key
	nodeBytes   []byte
	valueDigest maybe.Maybe[[]byte]
}

// Returns a new node with the given [key] and no value.
func newNode(key Key) *node {
	return &node{
		dbNode: dbNode{
			children: make(map[byte]*child, 2),
		},
		key: key,
	}
}

// Parse [nodeBytes] to a node and set its key to [key].
func parseNode(key Key, nodeBytes []byte) (*node, error) {
	n := dbNode{}
	if err := codec.decodeDBNode(nodeBytes, &n); err != nil {
		return nil, err
	}
	result := &node{
		dbNode:    n,
		key:       key,
		nodeBytes: nodeBytes,
	}

	result.setValueDigest()
	return result, nil
}

// Returns true iff this node has a value.
func (n *node) hasValue() bool {
	return !n.value.IsNothing()
}

// Returns the byte representation of this node.
func (n *node) bytes() []byte {
	if n.nodeBytes == nil {
		n.nodeBytes = codec.encodeDBNode(&n.dbNode)
	}

	return n.nodeBytes
}

// clear the cached values that will need to be recalculated whenever the node changes
// for example, node ID and byte representation
func (n *node) onNodeChanged() {
	n.nodeBytes = nil
}

// Returns and caches the ID of this node.
func (n *node) calculateID(metrics merkleMetrics) ids.ID {
	metrics.HashCalculated()
	bytes := codec.encodeHashValues(n)
	return hashing.ComputeHash256Array(bytes)
}

// Set [n]'s value to [val].
func (n *node) setValue(val maybe.Maybe[[]byte]) {
	n.onNodeChanged()
	n.value = val
	n.setValueDigest()
}

func (n *node) setValueDigest() {
	if n.value.IsNothing() || len(n.value.Value()) < HashLength {
		n.valueDigest = n.value
	} else {
		n.valueDigest = maybe.Some(hashing.ComputeHash256(n.value.Value()))
	}
}

// Adds [child] as a child of [n].
// Assumes [child]'s key is valid as a child of [n].
// That is, [n.key] is a prefix of [child.key].
func (n *node) addChild(childNode *node, tokenSize int) {
	n.setChildEntry(
		childNode.key.Token(n.key.length, tokenSize),
		&child{
			compressedKey: childNode.key.Skip(n.key.length + tokenSize),
			hasValue:      childNode.hasValue(),
		},
	)
}

// Adds a child to [n] without a reference to the child node.
func (n *node) setChildEntry(index byte, childEntry *child) {
	n.onNodeChanged()
	n.children[index] = childEntry
}

// Removes [child] from [n]'s children.
func (n *node) removeChild(child *node, tokenSize int) {
	n.onNodeChanged()
	delete(n.children, child.key.Token(n.key.length, tokenSize))
}

// clone Returns a copy of [n].
// Note: value isn't cloned because it is never edited, only overwritten
// if this ever changes, value will need to be copied as well
// it is safe to clone all fields because they are only written/read while one or both of the db locks are held
func (n *node) clone() *node {
	result := &node{
		key: n.key,
		dbNode: dbNode{
			value:    n.value,
			children: make(map[byte]*child, len(n.children)),
		},
		valueDigest: n.valueDigest,
		nodeBytes:   n.nodeBytes,
	}
	for key, existing := range n.children {
		result.children[key] = &child{
			compressedKey: existing.compressedKey,
			id:            existing.id,
			hasValue:      existing.hasValue,
		}
	}
	return result
}

// Returns the ProofNode representation of this node.
func (n *node) asProofNode() ProofNode {
	pn := ProofNode{
		Key:         n.key,
		Children:    make(map[byte]ids.ID, len(n.children)),
		ValueOrHash: maybe.Bind(n.valueDigest, slices.Clone[[]byte]),
	}
	for index, entry := range n.children {
		pn.Children[index] = entry.id
	}
	return pn
}
