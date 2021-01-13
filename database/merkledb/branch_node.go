package merkledb

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
)

// BranchNode represents a Node with an array of Nodes and a SharedAddress
// SharedAddress is the shared prefix of all keys under this node
// Nodes are Addresses of other Nodes, which their addresses are suffixed by SharedAddress
// Hashes are hashes of child Nodes
// [ 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, a, b, c, d, f ]
//     [ x ] -> LeafNode
//     [ not found ] -> EmptyNode
//
type BranchNode struct {
	Nodes         [UnitSize][]Unit `json:"nodes"`
	Hashes        [UnitSize][]byte `json:"hashes"`
	SharedAddress []Unit           `json:"sharedAddress"`
	Parent        []Unit           `json:"parent,omitempty"`
	Type          string           `json:"type"`
	StoredHash    []byte           `json:"storedHash"`
}

// NewBranchNode returns a new BranchNode
func NewBranchNode(sharedAddress []Unit, parent Node) Node {
	return &BranchNode{
		SharedAddress: sharedAddress,
		Nodes:         [UnitSize][]Unit{},
		Parent:        parent.StorageKey(),
		Type:          "BranchNode",
	}
}

// GetChild returns a child node
// returns Node - one of it's children
// returns EmptyNode - no node in this position
//
func (b *BranchNode) GetChild(key []Unit) (Node, error) {
	if !EqualUnits(SharedPrefix(b.SharedAddress, key), b.SharedAddress) {
		return NewEmptyNode(b.StorageKey(), key), nil
	}

	// if the node CAN exist in this prefix but doesn't return an EmptyNode
	// Node Key: AABBCC
	// SharedAddress: AABBC
	// FirstNonPrefix:  AABBCC - AABBC = C
	nodeStorageKey := b.Nodes[FirstNonPrefix(b.SharedAddress, key)]
	if nodeStorageKey == nil {
		return NewEmptyNode(b.StorageKey(), key), nil
	}

	return Persistence.GetNodeByUnitKey(nodeStorageKey)
}

// GetNextNode returns the next node in increasing key order
// returns Node - one of it's children
func (b *BranchNode) GetNextNode(prefix []Unit, start []Unit, key []Unit) (Node, error) {

	// check the prefix
	if len(prefix) != 0 {
		// this branch isn't prefixed
		if !IsPrefixed(prefix, b.SharedAddress) {
			return NewEmptyNode(b.StorageKey(), key), nil
		}
	}

	// return the first, left-most child if the key is nil
	if key == nil {
		for _, nodeStorageKey := range b.Nodes {
			if nodeStorageKey != nil {
				// no prefix + no start return the left-most child
				if len(start) == 0 && len(prefix) == 0 {
					return Persistence.GetNodeByUnitKey(nodeStorageKey)
				}

				// check for the prefix
				nodeKey := FromStorageKey(nodeStorageKey)
				if len(prefix) != 0 && IsPrefixed(prefix, nodeKey) {
					if len(start) == 0 {
						return Persistence.GetNodeByUnitKey(nodeStorageKey)
					}
					if Greater(nodeKey, start) {
						return Persistence.GetNodeByUnitKey(nodeStorageKey)
					}
				}

				// check for a start
				// if it's a BranchNode it will return true and drill down that BranchNode
				if len(start) != 0 && Greater(nodeKey, start) {
					return Persistence.GetNodeByUnitKey(nodeStorageKey)
				}
			}
		}
		return NewEmptyNode(b.StorageKey(), key), nil
	}

	if !EqualUnits(SharedPrefix(b.SharedAddress, key), b.SharedAddress) {
		return NewEmptyNode(b.StorageKey(), key), nil
	}

	// search the next node after the address one
	for _, nodeStorageKey := range b.Nodes[FirstNonPrefix(b.SharedAddress, key):] {
		if nodeStorageKey != nil {
			// TODO Think theres a better way of doing this
			nodeKey := FromStorageKey(nodeStorageKey)
			if EqualUnits(nodeKey, key) {
				continue
			}
			// if its prefixed make sure the node prefix respects it
			if len(prefix) != 0 && !IsPrefixed(prefix, nodeKey) {
				continue
			}
			return Persistence.GetNodeByUnitKey(nodeStorageKey)
		}
	}
	// if the node CAN exist in this SharedAddress but doesn't, return an EmptyNode
	return NewEmptyNode(b.StorageKey(), key), nil
}

// Insert adds a new node in the branch
// if the node doesn't belong in the branch it request the Parent to insert it
// if there's already a node on that position it creates a BranchNode and branches the position out
// otherwise just inserts a LeafNode, rehashes itself and request the parent to rehash itself
func (b *BranchNode) Insert(key []Unit, value []byte) error {

	// if the node CAN'T exist in this prefix request the Parent to insert
	if !EqualUnits(SharedPrefix(b.SharedAddress, key), b.SharedAddress) {
		parent, err := Persistence.GetNodeByUnitKey(b.Parent)
		if err != nil {
			return err
		}

		// nothings changed in this node
		// insertion will trigger a rehash from the parent upwards
		return parent.Insert(key, value)
	}

	// if the position already exists then it's a new suffixed address
	// needs a new branchNode
	if nodeStorageKey := b.Nodes[FirstNonPrefix(b.SharedAddress, key)]; nodeStorageKey != nil {

		// node keys and node storage keys are different
		nodeKey := FromStorageKey(nodeStorageKey)
		newBranch := NewBranchNode(SharedPrefix(nodeKey, key), b)

		node, err := Persistence.GetNodeByUnitKey(nodeStorageKey)
		if err != nil {
			return err
		}

		// store the new branch address in the current branch before other operations
		// existing position is now the newBranch which holds the two nodes (existing + insert)
		b.Nodes[FirstNonPrefix(b.SharedAddress, key)] = newBranch.StorageKey()
		err = Persistence.StoreNode(b)
		if err != nil {
			return err
		}

		// existing node is added to the new BranchNode
		err = newBranch.SetChild(node)
		if err != nil {
			return err
		}

		// insertion will trigger a rehash from the newBranch upwards
		return newBranch.Insert(key, value)
	}

	// all good, insert a LeafNode
	newLeafNode, err := NewLeafNode(key, value, b)
	if err != nil {
		return err
	}

	b.Nodes[FirstNonPrefix(b.SharedAddress, key)] = newLeafNode.StorageKey()

	return b.Hash(key, newLeafNode.GetHash())
}

// Delete
// the delete request comes from a child node always (in the upwards direction)
// if the child is a LeafNode, it requests the Parent the deletion
// if the child is a BranchNode,
//     it either deletes + rehashes upwards
//     or if there's one Node left in the branch it requests Parent to take it + request the Parent rehash
//
func (b *BranchNode) Delete(key []Unit) error {

	// there's no node to delete here
	if nodeKey := b.Nodes[FirstNonPrefix(b.SharedAddress, key)]; nodeKey == nil {
		return database.ErrNotFound
	}

	// the child nodeKey that called the delete
	// is either a LeafNode or an empty BranchNode
	// remove it from the current branch
	b.Nodes[FirstNonPrefix(b.SharedAddress, key)] = nil

	// if the current BranchNode has exactly one children
	// request the Parent to take it
	if b.nodeLengthEquals(1) {
		var singleNode Node
		var err error
		for _, v := range b.Nodes {
			if v != nil {
				singleNode, err = Persistence.GetNodeByUnitKey(v)
				if err != nil {
					return err
				}
				break
			}
		}

		parent, err := Persistence.GetNodeByUnitKey(b.Parent)
		if err != nil {
			return err
		}

		err = parent.SetChild(singleNode)
		if err != nil {
			return err
		}

		err = parent.Hash(key, singleNode.GetHash())
		if err != nil {
			return err
		}

		return Persistence.DeleteNode(b)
	}

	// node was deleted rehash the current BranchNode + parents
	return b.Hash(key, nil)
}

// SetChild force sets a child in the BranchNode
// if the BranchNode only has one node we can link the Parent to the child and skip this node
func (b *BranchNode) SetChild(node Node) error {

	var parent Node
	var err error

	// useful for deletion
	if b.nodeLengthEquals(1) {
		parent, err = Persistence.GetNodeByUnitKey(b.Parent)
		if err != nil {
			return err
		}

		err = parent.SetChild(node)
		if err != nil {
			return err
		}
		err = Persistence.DeleteNode(b)
		if err != nil {
			return err
		}
		return nil
	}

	// do not rehash here as it will be followed by an Insert or a Delete
	// we also don't store here as the next call will do that for us - and the branch is in memory
	b.Nodes[FirstNonPrefix(b.SharedAddress, node.Key())] = node.StorageKey()
	b.Hashes[FirstNonPrefix(b.SharedAddress, node.Key())] = node.GetHash()

	return node.SetParent(b)
}

// SetParent force sets the Parent
func (b *BranchNode) SetParent(node Node) error {
	b.Parent = node.StorageKey()
	return Persistence.StoreNode(b)
}

// Value not used in the BranchNode
func (b *BranchNode) Value() []byte { return nil }

// Hash takes in a key and the hash for that key
// it sets the hash for that node in the correct position
// rehash the branch and requests the parent to do the same providing the BranchNodes key
func (b *BranchNode) Hash(nodeKey []Unit, hash []byte) error {

	b.Hashes[FirstNonPrefix(b.SharedAddress, nodeKey)] = hash

	hashSet := make([][]byte, UnitSize+1)
	hashSet[0] = ToExpandedBytes(b.SharedAddress)

	i := 1
	for _, childHash := range b.Hashes {
		hashSet[i] = childHash
		i++
	}

	b.StoredHash = Hash(hashSet...)
	err := Persistence.StoreNode(b)
	if err != nil {
		return err
	}

	parent, err := Persistence.GetNodeByUnitKey(b.Parent)
	if err != nil {
		return err
	}

	return parent.Hash(b.Key(), b.StoredHash)
}

func (b *BranchNode) GetHash() []byte {
	return b.StoredHash
}

// Key returns the BranchNode SharedAddress
func (b *BranchNode) Key() []Unit {
	return b.SharedAddress
}

// StorageKey returns the BranchNode SharedAddress suffixed with B-
func (b *BranchNode) StorageKey() []Unit {
	return append([]Unit("B-"), b.SharedAddress...)
}

// Print prints the node
func (b *BranchNode) Print() {
	fmt.Printf("Branch ID: %v - SharedAddress: %v - Parent: %v \n\t↪ Nodes: %v \n", b.StorageKey(), b.SharedAddress, b.Parent, b.Nodes)
	for _, nodeKey := range b.Nodes {
		if nodeKey != nil {
			node, err := Persistence.GetNodeByUnitKey(nodeKey)
			if err != nil {
				panic(err)
			}
			node.Print()
		}
	}
}

func (b *BranchNode) nodeLengthEquals(size int) bool {

	i := 0
	for _, v := range b.Nodes {
		if v != nil {
			i++
		}
	}
	return i == size
}
