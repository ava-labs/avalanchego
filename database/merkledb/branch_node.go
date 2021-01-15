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
	Nodes              [UnitSize][]byte `serialize:"true"`
	SharedAddress      []Unit           `serialize:"true"`
	StoredHash         []byte           `serialize:"true"`
	previousStoredHash []byte
	parent             Node
	persistence        *Persistence
}

// NewBranchNode returns a new BranchNode
func NewBranchNode(sharedAddress []Unit, parent Node, persistence *Persistence) Node {
	return &BranchNode{
		SharedAddress: sharedAddress,
		Nodes:         [UnitSize][]byte{},
		parent:        parent,
		persistence:   persistence,
	}
}

// GetChild returns a child node
// returns Node - one of it's children
// returns EmptyNode - no node in this position
//
func (b *BranchNode) GetChild(key []Unit) (Node, error) {
	if !EqualUnits(SharedPrefix(b.SharedAddress, key), b.SharedAddress) {
		return NewEmptyNode(b, key), nil
	}

	// if the node CAN exist in this prefix but doesn't return an EmptyNode
	// Node Key: AABBCC
	// SharedAddress: AABBC
	// FirstNonPrefix:  AABBCC - AABBC = C
	nodeHash := b.Nodes[FirstNonPrefix(b.SharedAddress, key)]
	if len(nodeHash) == 0 {
		return NewEmptyNode(b, key), nil
	}

	node, err := b.persistence.GetNodeByHash(nodeHash)
	if err != nil {
		return nil, err
	}
	node.SetParent(b)
	node.SetPersistence(b.persistence)

	return node, nil
}

// GetNextNode returns the next node in increasing key order
// returns Node - one of it's children
func (b *BranchNode) GetNextNode(prefix []Unit, start []Unit, key []Unit) (Node, error) {

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

	if !EqualUnits(SharedPrefix(b.SharedAddress, key), b.SharedAddress) {
		return NewEmptyNode(b, key), nil
	}

	// search the next node after the address one
	for _, nodeHash := range b.Nodes[FirstNonPrefix(b.SharedAddress, key):] {
		if len(nodeHash) != 0 {

			node, err := b.persistence.GetNodeByHash(nodeHash)
			if err != nil {
				return nil, err
			}

			// TODO Think theres a better way of doing this
			if EqualUnits(node.Key(), key) {
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
// if the node doesn't belong in the branch it request the Parent to insert it
// if there's already a node on that position it creates a BranchNode and branches the position out
// otherwise just inserts a LeafNode, rehashes itself and request the parent to rehash itself
func (b *BranchNode) Insert(key []Unit, value []byte) error {

	// if the node CAN'T exist in this prefix request the Parent to insert
	if !EqualUnits(SharedPrefix(b.SharedAddress, key), b.SharedAddress) {

		// nothings changed in this node
		// insertion will trigger a rehash from the parent upwards
		return b.parent.Insert(key, value)
	}

	// if the position already exists then it's a new suffixed address
	// needs a new branchNode
	if nodeHash := b.Nodes[FirstNonPrefix(b.SharedAddress, key)]; len(nodeHash) != 0 {

		node, err := b.persistence.GetNodeByHash(nodeHash)
		if err != nil {
			return err
		}

		// create a new branch with this BranchNode as the parent
		newBranch := NewBranchNode(SharedPrefix(node.Key(), key), b, b.persistence)

		// existing node is added to the new BranchNode
		err = newBranch.SetChild(node)
		if err != nil {
			return err
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

	b.Nodes[FirstNonPrefix(b.SharedAddress, key)] = newLeafNode.GetHash()

	err = b.Hash(key, newLeafNode.GetHash())
	if err != nil {
		return err
	}

	return nil
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

		err = b.parent.Hash(key, singleNode.GetHash())
		if err != nil {
			return err
		}

		return b.persistence.DeleteNode(b)
	}

	// node was deleted rehash the current BranchNode + parents
	return b.Hash(key, nil)
}

// SetChild force sets a child in the BranchNode
func (b *BranchNode) SetChild(node Node) error {
	// do not rehash here as it will be followed by an Insert or a Delete
	// we also don't store here as the next call will do that for us - and the branch is in memory
	b.Nodes[FirstNonPrefix(b.SharedAddress, node.Key())] = node.GetHash()

	return nil
}

// SetParent force sets the Parent
func (b *BranchNode) SetParent(node Node) {
	b.parent = node
}

// SetPersistence force sets the Persistenc
func (b *BranchNode) SetPersistence(p *Persistence) {
	b.persistence = p
}

// Value not used in the BranchNode
func (b *BranchNode) Value() []byte { return nil }

// Hash takes in a key and the hash for that key
// it sets the hash for that node in the correct position
// rehash the branch and requests the parent to do the same providing the BranchNodes key
func (b *BranchNode) Hash(nodeKey []Unit, hash []byte) error {

	b.Nodes[FirstNonPrefix(b.SharedAddress, nodeKey)] = hash

	hashSet := make([][]byte, UnitSize+1)
	hashSet[0] = ToExpandedBytes(b.SharedAddress)

	i := 1
	for _, childHash := range b.Nodes {
		hashSet[i] = childHash
		i++
	}

	b.previousStoredHash = b.StoredHash
	b.StoredHash = Hash(hashSet...)
	err := b.persistence.StoreNode(b)
	if err != nil {
		return err
	}

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

// Key returns the BranchNode SharedAddress
func (b *BranchNode) Key() []Unit {
	return b.SharedAddress
}

// Print prints the node
func (b *BranchNode) Print() {
	fmt.Printf("Branch ID: %x - SharedAddress: %v - Parent: %p \n\t↪ Nodes: ", b.GetHash(), b.SharedAddress, b.parent)
	fmt.Printf("[")
	for _, nodeHash := range b.Nodes {
		fmt.Printf("[%x]", nodeHash)
	}
	fmt.Printf("]\n")
	for _, nodeKey := range b.Nodes {
		if len(nodeKey) != 0 {
			node, err := b.persistence.GetNodeByHash(nodeKey)
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
		if len(v) != 0 {
			i++
		}
	}
	return i == size
}
