// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"crypto/sha256"
	"encoding/binary"
	"slices"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

const HashLength = 32

var indexToVarint = [BranchFactorLargest][]byte{}

func init() {
	for i := 0; i < int(BranchFactorLargest); i++ {
		var buf [binary.MaxVarintLen64]byte
		bufLen := binary.PutUvarint(buf[:], uint64(i))
		indexToVarint[i] = buf[:bufLen]
	}
}

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
		dbNode: n,
		key:    key,
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
	return codec.encodeDBNode(&n.dbNode)
}

// Returns and caches the ID of this node.
func (n *node) calculateID(metrics merkleMetrics) ids.ID {
	metrics.HashCalculated()

	var (
		sha  = sha256.New()
		hash ids.ID
	)

	var childrenLenVarInt []byte
	if len(n.children) < int(BranchFactorLargest) {
		childrenLenVarInt = indexToVarint[uint64(len(n.children))]
	} else {
		var buf [binary.MaxVarintLen64]byte
		len := binary.PutUvarint(buf[:], uint64(len(n.children)))
		childrenLenVarInt = buf[:len]
	}

	_, _ = sha.Write(childrenLenVarInt)

	// ensure that the order of entries is consistent
	keys := make([]byte, 0, BranchFactorLargest)
	for k := range n.children {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, index := range keys {
		entry := n.children[index]
		indexVarInt := indexToVarint[index]
		_, _ = sha.Write(indexVarInt)
		_, _ = sha.Write(entry.id[:])
	}

	if n.valueDigest.HasValue() {
		_, _ = sha.Write(trueBytes)
		value := n.valueDigest.Value()

		var valueLenVarInt []byte
		if len(value) < int(BranchFactorLargest) {
			valueLenVarInt = indexToVarint[uint64(len(value))]
		} else {
			var buf [binary.MaxVarintLen64]byte
			len := binary.PutUvarint(buf[:], uint64(len(value)))
			valueLenVarInt = buf[:len]
		}
		_, _ = sha.Write(valueLenVarInt)
		_, _ = sha.Write(value)
	} else {
		_, _ = sha.Write(falseBytes)
	}

	var keyLenVarInt []byte
	if n.key.length < int(BranchFactorLargest) {
		keyLenVarInt = indexToVarint[uint64(n.key.length)]
	} else {
		var buf [binary.MaxVarintLen64]byte
		len := binary.PutUvarint(buf[:], uint64(n.key.length))
		keyLenVarInt = buf[:len]
	}

	_, _ = sha.Write(keyLenVarInt)
	_, _ = sha.Write(n.key.Bytes())
	sha.Sum(hash[:0])
	return hash
}

// Set [n]'s value to [val].
func (n *node) setValue(val maybe.Maybe[[]byte]) {
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
	n.addChildWithID(childNode, tokenSize, ids.Empty)
}

func (n *node) addChildWithID(childNode *node, tokenSize int, childID ids.ID) {
	n.setChildEntry(
		childNode.key.Token(n.key.length, tokenSize),
		&child{
			compressedKey: childNode.key.Skip(n.key.length + tokenSize),
			id:            childID,
			hasValue:      childNode.hasValue(),
		},
	)
}

// Adds a child to [n] without a reference to the child node.
func (n *node) setChildEntry(index byte, childEntry *child) {
	n.children[index] = childEntry
}

// Removes [child] from [n]'s children.
func (n *node) removeChild(child *node, tokenSize int) {
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
