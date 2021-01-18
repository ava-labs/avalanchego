package merkledb

import (
	"bytes"
	"fmt"
)

// LeafNode is a representation of a Node
// it holds key, LeafValue and a Parent pointer
type LeafNode struct {
	LeafKey            Key    `serialize:"true"`
	LeafValue          []byte `serialize:"true"`
	StoredHash         []byte `serialize:"true"`
	Refs               int32  `serialize:"true"`
	previousStoredHash []byte
	parent             Node
	persistence        *Persistence
}

// NewLeafNode creates a new Leaf Node
func NewLeafNode(key Key, value []byte, parent Node, persistence *Persistence) (Node, error) {
	l := &LeafNode{
		LeafKey:     key,
		LeafValue:   value,
		parent:      parent,
		persistence: persistence,
	}

	return l, l.Hash(nil, nil)
}

func (l *LeafNode) GetChild(key Key) (Node, error) {
	return l, nil
}

// Insert in the a LeafNode means that it's either
// the same key - we update the LeafValue
// otherwise - request the Parent to insert the k/v
func (l *LeafNode) Insert(key Key, value []byte) error {
	// only the LeafValue changed - rehash + request the rehash upwards
	if key.Equals(l.LeafKey) {
		if bytes.Equal(l.LeafValue, value) {
			return nil
		}
		l.LeafValue = value

		err := l.Hash(nil, nil)
		if err != nil {
			return err
		}

		return l.parent.Hash(key, l.StoredHash)
	}

	// it's actually a new LeafNode
	return l.parent.Insert(key, value)
}

// GetNextNode returns itself
func (l *LeafNode) GetNextNode(prefix Key, start Key, key Key) (Node, error) {
	return l, nil
}

// Delete removes this LeafNode from the Parent
func (l *LeafNode) Delete(key Key) error {
	err := l.persistence.DeleteNode(l)
	if err != nil {
		return err
	}
	return l.parent.Delete(key)
}

// SetChild should never be called
func (l *LeafNode) SetChild(node Node) error { return nil }

// SetParent is used on specially when updating the tree ( skipping branchNodes)
func (l *LeafNode) SetParent(node Node) {
	l.parent = node
}

// SetPersistence force sets the persistence in the LeafNode
func (l *LeafNode) SetPersistence(persistence *Persistence) {
	l.persistence = persistence
}

// Value returns the stored LeafValue
func (l *LeafNode) Value() []byte {
	return l.LeafValue
}

func (l *LeafNode) Hash(key Key, hash []byte) error {
	newHash := Hash(l.LeafValue, l.LeafKey.ToExpandedBytes())
	if bytes.Equal(l.StoredHash, newHash) {
		return nil
	}

	l.previousStoredHash = l.StoredHash
	l.StoredHash = newHash

	return l.persistence.StoreNode(l)
}

// GetHash returns the StoredHash
func (l *LeafNode) GetHash() []byte {
	return l.StoredHash
}

// GetPreviousHash returns the previousStoredHash
// for deleting unused LeafNode from the DB
func (l *LeafNode) GetPreviousHash() []byte {
	return l.previousStoredHash
}

func (l *LeafNode) References(change int32) int32 {
	l.Refs += change
	return l.Refs
}

// Key returns the stored key
func (l *LeafNode) Key() Key {
	return l.LeafKey
}

// Print prints this Node data
func (l *LeafNode) Print() {
	fmt.Printf("Leaf ID: %x - Parent: %p - Key: %v - Val: %v\n", l.GetHash(), l.parent, l.LeafKey, l.LeafValue)
}
