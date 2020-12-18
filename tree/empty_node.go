package tree

import "fmt"

// EmptyNode is a Node implementation that represents non existing nodes
// it's used mainly when traversing the tree fetching a non existing key
// it pre-sets the conditions for insertion
type EmptyNode struct {
	parent        Node
	key           []Unit
	sharedAddress []Unit
}

// NewEmptyNode returns a new EmptyNode
// the Node will have
//    parent        -> typically the node that instantiates it
//    key           -> the address that is non existent in the tree
func NewEmptyNode(parent Node, key []Unit) Node {
	return &EmptyNode{
		parent: parent,
		key:    key,
	}
}

// GetChild should never be reached
func (e *EmptyNode) GetChild(key []Unit) Node { return nil }

// Insert requests it's parent to insert the k/v
func (e *EmptyNode) Insert(key []Unit, value []byte) {
	e.parent.Insert(key, value)
}

// Delete should never be called
func (e *EmptyNode) Delete(key []Unit) bool { return false }

// SetChild should never be called
func (e *EmptyNode) SetChild(node Node) {}

// SetParent should never be called
func (e *EmptyNode) SetParent(node Node) {}

// Value should never be called
func (e *EmptyNode) Value() []byte { return nil }

func (e *EmptyNode) Hash() {}

func (e *EmptyNode) GetHash() []byte { return nil }

// Key should never be called
func (e *EmptyNode) Key() []Unit {
	return e.key
}

// Print should never be called
func (e *EmptyNode) Print() {
	fmt.Printf("ERROR: should never be called EmptyNode ID: %p - Parent: %p", e, e.parent)
}
