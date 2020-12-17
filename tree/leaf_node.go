package tree

import (
	"fmt"
)

// LeafNode
//
// [key-end (16 byte), value]
//
type LeafNode struct {
	key    []Unit
	value  []byte
	parent Node
}

// NewLeafNode creates a new Leaf Node given
func NewLeafNode(key []Unit, value []byte, parent Node) *LeafNode {
	return &LeafNode{
		key:    key,
		value:  value,
		parent: parent,
	}
}

func (l *LeafNode) GetNode(key []Unit) Node {
	return l
}

func (l *LeafNode) GetChild(key []Unit) Node {
	return l
}
func (l *LeafNode) SetChild(node Node) {}

// Insert in the a LeafNode means that it's either
// the same key - we update the value
// a shared key - we create a new BranchNode
func (l *LeafNode) Insert(key []Unit, value []byte) {
	if EqualUnits(l.key, key) {
		l.value = value
		return
		// TODO UPDATE HASH UPWARDS
	}

	l.parent.Insert(key, value)

	branch := NewBranchNode(SharedPrefix(l.key, key), l.parent)
	// TODO multiple insert + calc
	branch.Insert(l.key, l.value)
	branch.Insert(key, value)

	l.parent.SetChild(branch)
}

func (l *LeafNode) Print() {
	fmt.Printf("Leaf ID: %p - Parent: %p - Key: %v - Val: %v\n", &l, l.parent, l.key, l.value)
}

func (l *LeafNode) Parent() Node {
	return l.parent
}

func (l *LeafNode) Link(address []Unit, node Node) {}

func (l *LeafNode) Value() []byte {
	return l.value
}

func (l *LeafNode) Delete(key []Unit) bool {
	return l.parent.Delete(key)
}

func (l *LeafNode) Key() []Unit {
	return l.key
}

func (l *LeafNode) SetParent(node Node) {}
