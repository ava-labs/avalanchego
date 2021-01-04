package tree

import (
	"fmt"
)

// LeafNode is a representation of a Node
// it holds key, value and a parent pointer
type LeafNode struct {
	key    []Unit
	value  []byte
	parent Node
	hash   []byte
}

// NewLeafNode creates a new Leaf Node
func NewLeafNode(key []Unit, value []byte, parent Node) *LeafNode {
	l := &LeafNode{
		key:    key,
		value:  value,
		parent: parent,
	}
	l.Hash()

	return l
}

func (l *LeafNode) GetChild(key []Unit) Node {
	return l
}

// Insert in the a LeafNode means that it's either
// the same key - we update the value
// otherwise - request the parent to insert the k/v
func (l *LeafNode) Insert(key []Unit, value []byte) {
	if EqualUnits(l.key, key) {
		l.value = value

		// only the value changed - rehash + request the rehash upwards
		l.Hash()
		l.parent.Hash()
		return
	}

	l.parent.Insert(key, value)
}

// GetNextNode returns itself
func (l *LeafNode) GetNextNode(prefix []Unit, start []Unit, key []Unit) Node {
	return l
}

// Delete removes this LeafNode from the parent
func (l *LeafNode) Delete(key []Unit) bool {
	return l.parent.Delete(key)
}

// SetChild should never be called
func (l *LeafNode) SetChild(node Node) {}

// SetParent is used on specially when updating the tree ( skipping branchNodes)
func (l *LeafNode) SetParent(node Node) {
	l.parent = node
}

// Value returns the stored value
func (l *LeafNode) Value() []byte {
	return l.value
}

func (l *LeafNode) Hash() {
	l.hash = Hash(l.value, ToBytes(l.key))
	l.parent.Hash()
}

func (l *LeafNode) GetHash() []byte {
	return l.hash
}

// Key returns the stored key
func (l *LeafNode) Key() []Unit {
	return l.key
}

// Print prints this Node data
func (l *LeafNode) Print() {
	fmt.Printf("Leaf ID: %p - Parent: %p - Key: %v - Val: %v\n", &l, l.parent, l.key, l.value)
}
