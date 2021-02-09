package merkledb

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
)

// BranchNode represents a Node with an array of Nodes and a SharedAddress
// SharedAddress is the shared prefix of all keys under this node
// Nodes are Addresses of other Nodes, which their addresses are suffixed by SharedAddress
//
// LASTPOSITION is always a LeafNode - it's the special case where
// the LeafNode has the same Address as the BranchNode SharedAddress
//
// Hashes are the hashes of child Nodes
// [ 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, a, b, c, d, f, LASTPOSITION ]
//     [ x ] -> LeafNode
//     [ not found ] -> EmptyNode
//     [ LASTPOSITION ] -> LeafNode
//
type BranchNode struct {
	Nodes              [UnitSize][]byte `serialize:"true"`
	SharedAddress      Key              `serialize:"true"`
	StoredHash         []byte           `serialize:"true"`
	Refs               int32            `serialize:"true"`
	pivotNode          *Pivot
	previousStoredHash []byte
	parent             Node
	persistence        Persistence
}

// NewBranchNode returns a new BranchNode
func NewBranchNode(sharedAddress Key, parent Node, persistence Persistence) Node {
	return &BranchNode{
		SharedAddress: sharedAddress,
		Nodes:         [UnitSize][]byte{},
		parent:        parent,
		persistence:   persistence,
		Refs:          0,
		pivotNode:     NewPivot(),
	}
}

// GetChild returns a child node that should contain the Key
// returns Node - one of it's children
// returns EmptyNode - no node in this position
//
func (b *BranchNode) GetChild(key Key) (Node, error) {

	// checks if this BranchNode is the Pivot
	if b.Refs > 1 {
		b.PivotPoint().CheckAndSet(b.GetHash(), b.Refs)
	}

	if !key.ContainsPrefix(b.SharedAddress) {
		e := NewEmptyNode(b, key)
		e.PivotPoint().Copy(b.PivotPoint())
		return e, nil
	}

	// if the node CAN exist in this prefix but doesn't return an EmptyNode
	// Node Key: AABBCC
	// SharedAddress: AABBC
	// TerminationUnit:  AABBCC - AABBC = C
	nodeHash := b.Nodes[TerminationUnit(b.SharedAddress, key)]
	if len(nodeHash) == 0 {
		e := NewEmptyNode(b, key)
		e.PivotPoint().Copy(b.PivotPoint())
		return e, nil
	}

	node, err := b.persistence.GetNodeByHash(nodeHash)
	if err != nil {
		return nil, err
	}
	node.SetParent(b)

	// Propagates the Pivot to the child
	node.PivotPoint().Copy(b.PivotPoint())

	return node, nil
}

// GetNextNode returns the next node in increasing key order
// returns Node - one of it's children
func (b *BranchNode) GetNextNode(prefix Key, start Key, key Key) (Node, error) {
	// check the prefix
	if len(prefix) != 0 {
		// this branch isn't prefixed
		if !IsPrefixed(prefix, b.SharedAddress) {
			return NewEmptyNode(b, key), nil
		}
	}

	// return the first, left-most child if the key is nil
	if key == nil {
		for _, nodeHash := range b.Nodes {
			if len(nodeHash) != 0 {
				node, err := b.persistence.GetNodeByHash(nodeHash)
				if err != nil {
					return nil, err
				}
				// no prefix + no start return the left-most child
				if len(start) == 0 && len(prefix) == 0 {
					return node, nil
				}

				// check for the prefix
				if len(prefix) != 0 && IsPrefixed(prefix, node.Key()) {
					if len(start) == 0 {
						return node, nil
					}
					if Greater(node.Key(), start) {
						return node, nil
					}
				}

				// check for a start
				// if it's a BranchNode it will return true and drill down that BranchNode
				if len(start) != 0 && Greater(node.Key(), start) {
					return node, nil
				}
			}
		}
		return NewEmptyNode(b, key), nil
	}

	if !key.ContainsPrefix(b.SharedAddress) {
		return NewEmptyNode(b, key), nil
	}

	// search the next node after the address one
	for _, nodeHash := range b.Nodes[TerminationUnit(b.SharedAddress, key):] {
		if len(nodeHash) != 0 {

			node, err := b.persistence.GetNodeByHash(nodeHash)
			if err != nil {
				return nil, err
			}

			// TODO Think theres a better way of doing this
			if node.Key().Equals(key) {
				continue
			}
			// if its prefixed make sure the node prefix respects it
			if len(prefix) != 0 && !IsPrefixed(prefix, node.Key()) {
				continue
			}
			return node, nil
		}
	}
	// if the node CAN exist in this SharedAddress but doesn't, return an EmptyNode
	return NewEmptyNode(b, key), nil
}

// Insert adds a new node in the branch
//
// If the node doesn't belong in this branch  - Request the Parent to insert it
// If there's already a node in that position - Create a BranchNode with both existing and new LeafNodes
// and insert in that position
// Otherwise just inserts a LeafNode on the empty position
// On each operation where this BranchNode changed - Rehash itself and request the parent to rehash
func (b *BranchNode) Insert(key Key, value []byte) error {

	// if the node CAN'T exist in this prefix request the Parent to insert
	if !key.ContainsPrefix(b.SharedAddress) {
		// nothing changes in this node
		// insertion will trigger a rehash from the parent upwards
		return b.parent.Insert(key, value)
	}

	// if the position already exists then create a new BranchNode
	// with both values and insert it, in this position
	if nodeHash := b.Nodes[TerminationUnit(b.SharedAddress, key)]; len(nodeHash) != 0 {

		// get the existing node
		node, err := b.persistence.GetNodeByHash(nodeHash)
		if err != nil {
			return err
		}

		// create a new branch with the current BranchNode as the parent
		newBranch := NewBranchNode(SharedPrefix(node.Key(), key), b, b.persistence)

		// existing node is added to the new BranchNode
		err = newBranch.SetChild(node)
		if err != nil {
			return err
		}

		// propagate the PivotPoint
		newBranch.PivotPoint().Copy(b.PivotPoint())

		// special case where a new BranchNode is created
		// only update the existing node refs in nodes after the PivotPoint was reached
		if newBranch.PivotPoint().reached && !newBranch.PivotPoint().Equals(nodeHash) {
			node.References(1)
			err = b.persistence.StoreNode(node, true)
			if err != nil {
				return err
			}
		}

		// insertion will trigger a rehash from the newBranch upwards
		// NewBranch: Insert - Hash - hash - Store - Parent.Hash: hash - Store - Parent.Hash, etc
		return newBranch.Insert(key, value)
	}

	// all good, insert a LeafNode
	newLeafNode, err := NewLeafNode(key, value, b, b.persistence)
	if err != nil {
		return err
	}

	newLeafHash := newLeafNode.GetHash()
	b.Nodes[TerminationUnit(b.SharedAddress, key)] = newLeafHash

	return b.Hash(key, newLeafHash)
}

// Delete
// the delete request comes from a child node (in the upwards direction)
// if the child is a LeafNode, it requests the Parent the deletion
// if the child is a BranchNode,
//     it either deletes + rehashes upwards
//     or if there's one Node left in the branch it requests Parent to take it + request the Parent rehash
//
func (b *BranchNode) Delete(key Key) error {

	// there's no node to delete here
	prefixedKey := TerminationUnit(b.SharedAddress, key)
	if nodeKey := b.Nodes[prefixedKey]; nodeKey == nil {
		return database.ErrNotFound
	}

	// the child nodeKey that called the delete
	// is either a LeafNode or an empty BranchNode
	// remove it from the current branch
	b.Nodes[prefixedKey] = nil

	// if the current BranchNode has exactly one children
	// request the Parent to take it
	if b.nodeLengthEquals(1) {
		var singleNode Node
		var err error
		for _, v := range b.Nodes {
			if len(v) != 0 {
				singleNode, err = b.persistence.GetNodeByHash(v)
				if err != nil {
					return err
				}
				break
			}
		}

		err = b.parent.SetChild(singleNode)
		if err != nil {
			return err
		}

		// deleting the node will also update the pivot if it needs to be
		err = b.persistence.DeleteNode(b)
		if err != nil {
			return err
		}

		b.parent.PivotPoint().Copy(b.pivotNode)

		return b.parent.Hash(key, singleNode.GetHash())
	}

	// node was deleted from the branch
	// rehash the current BranchNode + parents
	return b.Hash(key, nil)
}

// SetChild force sets a child in the BranchNode
func (b *BranchNode) SetChild(node Node) error {
	// do not rehash here as it will be followed by an Insert or a Delete
	// we also don't store here as the next call will do that for us - and the branch is in memory
	b.Nodes[TerminationUnit(b.SharedAddress, node.Key())] = node.GetHash()

	return nil
}

// SetParent force sets the Parent
func (b *BranchNode) SetParent(node Node) {
	b.parent = node
}

// SetPersistence force sets the Persistence
func (b *BranchNode) SetPersistence(p Persistence) {
	b.persistence = p
}

// Value not used in the BranchNode
func (b *BranchNode) Value() []byte { return nil }

// Hash takes in a key and the hash for that key
// it sets the hash for that node in the correct position
// rehash the branch and requests the parent to do the same providing the BranchNodes key
func (b *BranchNode) Hash(nodeKey Key, hash []byte) error {

	b.Nodes[TerminationUnit(b.SharedAddress, nodeKey)] = hash

	hashSet := make([][]byte, UnitSize+1+1)
	hashSet[0] = b.SharedAddress.ToExpandedBytes()

	i := 1
	for _, childHash := range b.Nodes {
		hashSet[i] = childHash
		i++
	}

	b.previousStoredHash = b.StoredHash
	b.StoredHash = Hash(hashSet...)

	// Hashing creates a new Node - since it has a parent (known because of the call stack) mark it with 1 Reference
	b.Refs = 1
	err := b.persistence.StoreNode(b, false)
	if err != nil {
		return err
	}

	// propagate the Pivot upwards
	b.parent.PivotPoint().Copy(b.PivotPoint())
	return b.parent.Hash(b.Key(), b.StoredHash)
}

// GetHash returns the StoredHash
func (b *BranchNode) GetHash() []byte {
	return b.StoredHash
}

// GetPreviousHash returns the previousStoredHash
// for deleting unused BranchNode from the DB
func (b *BranchNode) GetPreviousHash() []byte {
	return b.previousStoredHash
}

// References increases and returns the number of references
// providing a get and a set operation in one method
func (b *BranchNode) References(change int32) int32 {
	b.Refs += change
	return b.Refs
}

// PivotPoint returns either the stored Pivot or a new Pivot
func (b *BranchNode) PivotPoint() *Pivot {
	if b.pivotNode == nil {
		b.pivotNode = NewPivot()
	}
	return b.pivotNode
}

// Key returns the BranchNode SharedAddress
func (b *BranchNode) Key() Key {
	return b.SharedAddress
}

// GetChildrenHashes returns the BranchNode Child Hashes
func (b *BranchNode) GetChildrenHashes() [][]byte {
	var children [][]byte
	for _, hash := range b.Nodes {
		if len(hash) != 0 {
			children = append(children, hash)
		}
	}
	return children
}

// GetReHash is mainly used for consistency checks
func (b *BranchNode) GetReHash() []byte {
	hashSet := make([][]byte, UnitSize+1)
	hashSet[0] = b.SharedAddress.ToExpandedBytes()

	i := 1
	for _, childHash := range b.Nodes {
		hashSet[i] = childHash
		i++
	}

	return Hash(hashSet...)
}

// Clear deletes all nodes attached to this BranchNode
func (b *BranchNode) Clear() error {
	for _, nodeHash := range b.GetChildrenHashes() {
		if len(nodeHash) == 0 {
			continue
		}
		child, err := b.persistence.GetNodeByHash(nodeHash)
		if err != nil {
			return err
		}

		if b.Refs > 1 {
			b.PivotPoint().CheckAndSet(b.GetHash(), b.Refs)
		}

		// Passes on the parents Refs if they are greater than the children
		child.PivotPoint().Copy(b.PivotPoint())

		err = child.Clear()
		if err != nil {
			return err
		}
	}

	return b.persistence.DeleteNode(b)
}

// String converts the node in a string format
func (b *BranchNode) String() string {
	nodes := "[\n"
	for _, nodeHash := range b.Nodes {
		if len(nodeHash) > 0 {
			nodes += fmt.Sprintf("\t\t[%x]\n", nodeHash)
		}
	}
	nodes += "\t\t]\n"
	return fmt.Sprintf("Branch ID: %x - SharedAddress: %v - Refs: %d \n\t↪ Nodes:%v", b.GetHash(), b.SharedAddress, b.Refs, nodes)
}

func (b *BranchNode) nodeLengthEquals(size int) bool {
	i := 0
	for _, v := range b.Nodes {
		if len(v) != 0 {
			i++
		}
	}
	return i == size
}
