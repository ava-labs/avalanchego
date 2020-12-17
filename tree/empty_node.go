package tree

import "fmt"

type EmptyNode struct {
	parent        Node
	key           []Unit
	sharedAddress []Unit
}

func NewEmptyNode(parent Node, key []Unit, sharedAddress []Unit) Node {
	return &EmptyNode{
		parent:        parent,
		key:           key,
		sharedAddress: sharedAddress,
	}
}

func (e *EmptyNode) GetChild(key []Unit) Node {
	return nil
}

func (e *EmptyNode) SetChild(node Node) {
}

// Insert inserts a LeafNode or a BranchNode in the tree
//
func (e *EmptyNode) Insert(key []Unit, value []byte) {
	// if there's no shared address it belongs to the parent
	if e.sharedAddress == nil {
		e.parent.Insert(key, value)
		return
	}

	// the node belongs to the parent
	e.parent.Insert(key, value)
}

func (e *EmptyNode) Print() {
	fmt.Printf("Empty - %v - %v\n", e.key, e.parent)
}

func (e *EmptyNode) Value() []byte { return nil }

func (e *EmptyNode) Delete(key []Unit) bool { return true }

func (e *EmptyNode) Key() []Unit {
	return e.key
}

func (e *EmptyNode) SetParent(node Node) {}
