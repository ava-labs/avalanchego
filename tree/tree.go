package tree

import (
	"fmt"
)

type Tree struct {
	rootNode Node
	rootHash []byte
}

func NewTree() *Tree {
	return &Tree{
		rootNode: nil,
	}
}

func (t *Tree) Root() []byte {
	return t.rootHash
}

func (t *Tree) Get(key []byte) ([]byte, bool) {

	node := t.findNode(FromBytes(key), t.rootNode)
	if _, ok := node.(*EmptyNode); ok {
		return nil, false
	}
	return node.Value(), true
}

// Put
// - When inserting in a EmptyNode replace it with a LeafNode
// - When inserting in a BranchNode
func (t *Tree) Put(key []byte, value []byte) {
	unitKey := FromBytes(key)
	if t.rootNode == nil {
		t.rootNode = NewRootNode(nil)
		t.rootNode.SetChild(NewLeafNode(unitKey, value, t.rootNode))
		return
	}

	insertNode := t.findNode(unitKey, t.rootNode)
	insertNode.Insert(unitKey, value)
}

func (t *Tree) Del(key []byte) bool {
	unitKey := FromBytes(key)

	deleteNode := t.findNode(unitKey, t.rootNode)
	return deleteNode.Delete(unitKey)
}

func (t *Tree) findNode(key []Unit, node Node) Node {

	switch node.(type) {
	case *EmptyNode:
		return node
	case *LeafNode:
		return node
	}
	if node == nil {
		fmt.Println("This aint right")
	}
	return t.findNode(key, node.GetChild(key))
}

func (t *Tree) PrintTree() {
	t.rootNode.Print()
}
