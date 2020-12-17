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

func (e *EmptyNode) Parent() Node {
	return e.parent
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

	prefix := SharedPrefix(e.sharedAddress, key)

	// the new branch belongs above the existing branch
	if len(e.sharedAddress) > len(prefix) {
		parent := e.parent
		for {
			parent = parent.Parent()
			if len(parent.Key()) <= len(SharedPrefix(parent.Key(), key)) {
				branch := NewBranchNode(prefix, parent)
				branch.Insert(key, value)
				branch.Link(e.sharedAddress, e.parent)
				// TODO change this no ?
				parentBranch, _ := e.parent.(*BranchNode)
				parentBranch.parent = branch
				parent.SetChild(branch)
				return
			}
		}
	}

	// the new branch belongs under the existing branch
	branch := NewBranchNode(prefix, e.parent)
	branch.Insert(key, value)
	e.parent.SetChild(branch)
}

func (e *EmptyNode) Print() {
	fmt.Printf("Empty - %v - %v\n", e.key, e.parent)
}

func (e *EmptyNode) Link(address []Unit, node Node) {}

func (e *EmptyNode) Value() []byte { return nil }

func (e *EmptyNode) Delete(key []Unit) bool { return true }

func (e *EmptyNode) Key() []Unit {
	return e.key
}

func (e *EmptyNode) SetParent(node Node) {}
