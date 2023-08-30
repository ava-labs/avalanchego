// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
	"unsafe"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

const (
	NodeBranchFactor = 16
	HashLength       = 32
	intSize          = int(unsafe.Sizeof(0))
	boolSize         = int(unsafe.Sizeof(true))
	byteSize         = 1
)

// the values that go into the node's id
type hashValues struct {
	Children map[byte]child
	Value    maybe.Maybe[[]byte]
	Key      SerializedPath
}

// Representation of a node stored in the database.
type dbNode struct {
	value    maybe.Maybe[[]byte]
	children map[byte]child
}

type child struct {
	compressedPath path
	id             ids.ID
	hasValue       bool
}

// node holds additional information on top of the dbNode that makes calculations easier to do
type node struct {
	dbNode
	id          ids.ID
	key         path
	nodeBytes   []byte
	valueDigest maybe.Maybe[[]byte]
	size        int
}

// Returns a new node with the given [key] and no value.
// If [parent] isn't nil, the new node is added as a child of [parent].
func newNode(parent *node, key path) *node {
	newNode := &node{
		dbNode: dbNode{
			children: make(map[byte]child, NodeBranchFactor),
		},
		key: key,
	}
	newNode.size = len(key) + boolSize + HashLength + intSize
	if parent != nil {
		parent.addChild(newNode)
	}
	return newNode
}

// Parse [nodeBytes] to a node and set its key to [key].
func parseNode(key path, nodeBytes []byte) (*node, error) {
	n := dbNode{}
	if err := codec.decodeDBNode(nodeBytes, &n); err != nil {
		return nil, err
	}
	result := &node{
		dbNode:    n,
		key:       key,
		nodeBytes: nodeBytes,
	}

	result.size = len(n.value.Value()) + len(result.key) + boolSize + HashLength + intSize
	for _, c := range result.children {
		result.size += byteSize + HashLength + len(c.compressedPath)
	}

	result.setValueDigest()
	return result, nil
}

// Returns true iff this node has a value.
func (n *node) hasValue() bool {
	return !n.value.IsNothing()
}

// Returns the byte representation of this node.
func (n *node) marshal() []byte {
	if n.nodeBytes == nil {
		n.nodeBytes = codec.encodeDBNode(&n.dbNode)
	}

	return n.nodeBytes
}

// clear the cached values that will need to be recalculated whenever the node changes
// for example, node ID and byte representation
func (n *node) onNodeChanged() {
	n.id = ids.Empty
	n.nodeBytes = nil

}

// Returns and caches the ID of this node.
func (n *node) calculateID(metrics merkleMetrics) error {
	if n.id != ids.Empty {
		return nil
	}

	hv := &hashValues{
		Children: n.children,
		Value:    n.valueDigest,
		Key:      n.key.Serialize(),
	}

	bytes := codec.encodeHashValues(hv)
	metrics.HashCalculated()
	n.id = hashing.ComputeHash256Array(bytes)
	return nil
}

// Set [n]'s value to [val].
func (n *node) setValue(val maybe.Maybe[[]byte]) {
	n.onNodeChanged()
	n.size -= len(n.value.Value())
	n.value = val
	n.size += len(val.Value())
	n.setValueDigest()
}

func (n *node) setValueDigest() {
	if n.value.IsNothing() || len(n.value.Value()) < HashLength {
		n.valueDigest = n.value
	} else {
		n.size -= len(n.valueDigest.Value())
		n.valueDigest = maybe.Some(hashing.ComputeHash256(n.value.Value()))
		n.size += len(n.valueDigest.Value())
	}
}

// Adds [child] as a child of [n].
// Assumes [child]'s key is valid as a child of [n].
// That is, [n.key] is a prefix of [child.key].
func (n *node) addChild(child *node) {
	n.addChildWithoutNode(
		child.key[len(n.key)],
		child.key[len(n.key)+1:],
		child.id,
		child.hasValue(),
	)
}

// Adds a child to [n] without a reference to the child node.
func (n *node) addChildWithoutNode(index byte, compressedPath path, childID ids.ID, hasValue bool) {
	n.onNodeChanged()
	if existing, ok := n.children[index]; ok {
		n.size -= byteSize + HashLength + len(existing.compressedPath)
	}
	n.children[index] = child{
		compressedPath: compressedPath,
		id:             childID,
		hasValue:       hasValue,
	}

	n.size += byteSize + HashLength + len(compressedPath)
}

// Removes [child] from [n]'s children.
func (n *node) removeChild(child *node) {
	n.onNodeChanged()
	index := child.key[len(n.key)]
	if existing, ok := n.children[index]; ok {
		n.size -= byteSize + HashLength + len(existing.compressedPath)
		delete(n.children, index)
	}
}

// clone Returns a copy of [n].
// Note: value isn't cloned because it is never edited, only overwritten
// if this ever changes, value will need to be copied as well
func (n *node) clone() *node {
	return &node{
		id:  n.id,
		key: n.key,
		dbNode: dbNode{
			value:    n.value,
			children: maps.Clone(n.children),
		},
		valueDigest: n.valueDigest,
		size:        n.size,
		nodeBytes:   n.nodeBytes,
	}
}

// Returns the ProofNode representation of this node.
func (n *node) asProofNode() ProofNode {
	pn := ProofNode{
		KeyPath:     n.key.Serialize(),
		Children:    make(map[byte]ids.ID, len(n.children)),
		ValueOrHash: maybe.Bind(n.valueDigest, slices.Clone[[]byte]),
	}
	for index, entry := range n.children {
		pn.Children[index] = entry.id
	}
	return pn
}
