// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

const NodeBranchFactor = 16

// the values that go into the node's id
type hashValues struct {
	Children map[byte]child
	Value    Maybe[[]byte]
	Key      SerializedPath
}

// Representation of a node stored in the database.
type dbNode struct {
	value    Maybe[[]byte]
	children map[byte]child
}

type child struct {
	compressedPath path
	id             ids.ID
}

// node holds additional information on top of the dbNode that makes calulcations easier to do
type node struct {
	dbNode
	id        ids.ID
	key       path
	nodeBytes []byte
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
	if parent != nil {
		parent.addChild(newNode)
	}
	return newNode
}

// Parse [nodeBytes] to a node and set its key to [key].
func parseNode(key path, nodeBytes []byte) (*node, error) {
	n := dbNode{}
	if _, err := Codec.decodeDBNode(nodeBytes, &n); err != nil {
		return nil, err
	}
	return &node{
		dbNode:    n,
		key:       key,
		nodeBytes: nodeBytes,
	}, nil
}

// Returns true iff this node has a value.
func (n *node) hasValue() bool {
	return !n.value.IsNothing()
}

// Returns the byte representation of this node.
func (n *node) marshal() ([]byte, error) {
	if n.nodeBytes != nil {
		return n.nodeBytes, nil
	}

	nodeBytes, err := Codec.encodeDBNode(Version, &(n.dbNode))
	if err != nil {
		return nil, err
	}
	n.nodeBytes = nodeBytes
	return n.nodeBytes, nil
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
		Value:    n.value,
		Key:      n.key.Serialize(),
	}
	bytes, err := Codec.encodeHashValues(Version, hv)
	if err != nil {
		return err
	}

	metrics.HashCalculated()
	n.id = hashing.ComputeHash256Array(bytes)
	return nil
}

// Set [n]'s value to [val].
func (n *node) setValue(val Maybe[[]byte]) {
	n.onNodeChanged()
	n.value = val
}

// Adds [child] as a child of [n].
// Assumes [child]'s key is valid as a child of [n].
// That is, [n.key] is a prefix of [child.key].
func (n *node) addChild(child *node) {
	n.addChildWithoutNode(
		child.key[len(n.key)],
		child.key[len(n.key)+1:],
		child.id,
	)
}

// Adds a child to [n] without a reference to the child node.
func (n *node) addChildWithoutNode(index byte, compressedPath path, childID ids.ID) {
	n.onNodeChanged()
	n.children[index] = child{
		compressedPath: compressedPath,
		id:             childID,
	}
}

// Returns the path of the only child of this node.
// Assumes this node has exactly one child.
func (n *node) getSingleChildPath() path {
	for index, entry := range n.children {
		return n.key + path(index) + entry.compressedPath
	}
	return ""
}

// Removes [child] from [n]'s children.
func (n *node) removeChild(child *node) {
	n.onNodeChanged()
	delete(n.children, child.key[len(n.key)])
}

// Returns a copy of [n].
func (n *node) clone() *node {
	result := *n
	result.children = maps.Clone(n.children)
	return &result
}

// Returns the ProofNode representation of this node.
func (n *node) asProofNode() ProofNode {
	pn := ProofNode{
		KeyPath:  n.key.Serialize(),
		Children: make(map[byte]ids.ID, len(n.children)),
		Value:    n.value,
	}
	for index, entry := range n.children {
		pn.Children[index] = entry.id
	}
	return pn
}
