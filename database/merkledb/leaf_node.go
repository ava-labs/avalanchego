package merkledb

import (
	"fmt"
)

// LeafNode is a representation of a Node
// it holds key, LeafValue and a Parent pointer
type LeafNode struct {
	LeafKey   []Unit `json:"LeafKey"`
	LeafValue []byte `json:"LeafValue"`
	Parent    []Unit `json:"Parent"`
	LeafHash  []byte `json:"LeafHash"`
	Type      string `json:"type"`
}

// NewLeafNode creates a new Leaf Node
func NewLeafNode(key []Unit, value []byte, parent Node) (Node, error) {
	l := &LeafNode{
		LeafKey:   key,
		LeafValue: value,
		Parent:    parent.StorageKey(),
		Type:      "LeafNode",
	}

	return l, l.Hash(nil, nil)
}

func (l *LeafNode) GetChild(key []Unit) (Node, error) {
	return l, nil
}

// Insert in the a LeafNode means that it's either
// the same key - we update the LeafValue
// otherwise - request the Parent to insert the k/v
func (l *LeafNode) Insert(key []Unit, value []byte) error {
	parent, err := Persistence.GetNodeByUnitKey(l.Parent)
	if err != nil {
		return err
	}

	// only the LeafValue changed - rehash + request the rehash upwards
	if EqualUnits(l.LeafKey, key) {
		l.LeafValue = value

		err = l.Hash(nil, nil)
		if err != nil {
			return err
		}

		return parent.Hash(key, l.LeafHash)
	}

	// it's actually a new LeafNode
	return parent.Insert(key, value)
}

// GetNextNode returns itself
func (l *LeafNode) GetNextNode(prefix []Unit, start []Unit, key []Unit) (Node, error) {
	return l, nil
}

// Delete removes this LeafNode from the Parent
func (l *LeafNode) Delete(key []Unit) bool {
	parent, err := Persistence.GetNodeByUnitKey(l.Parent)
	if err != nil {
		return false
	}
	err = Persistence.DeleteNode(l)
	if err != nil {
		return false
	}
	return parent.Delete(key)
}

// SetChild should never be called
func (l *LeafNode) SetChild(node Node) error { return nil }

// SetParent is used on specially when updating the tree ( skipping branchNodes)
func (l *LeafNode) SetParent(node Node) error {
	l.Parent = node.StorageKey()
	return Persistence.StoreNode(l)
}

// Value returns the stored LeafValue
func (l *LeafNode) Value() []byte {
	return l.LeafValue
}

func (l *LeafNode) Hash(key []Unit, hash []byte) error {
	l.LeafHash = Hash(l.LeafValue, ToExpandedBytes(l.LeafKey))
	return Persistence.StoreNode(l)
}

func (l *LeafNode) GetHash() []byte {
	return l.LeafHash
}

// Key returns the stored key
func (l *LeafNode) Key() []Unit {
	return l.LeafKey
}

// StorageKey returns the stored key suffix with L-
func (l *LeafNode) StorageKey() []Unit {
	return append([]Unit("L-"), l.LeafKey...)
}

// Print prints this Node data
func (l *LeafNode) Print() {
	fmt.Printf("Leaf ID: %v - Parent: %v - Key: %v - Val: %v\n", &l, l.Parent, l.LeafKey, l.LeafValue)
}
