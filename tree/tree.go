package tree

import "fmt"

// Tree holds the tree data
type Tree struct {
	rootNode Node
}

// NewTree returns a new instance of the Tree
func NewTree() *Tree {
	return &Tree{
		rootNode: NewRootNode(),
	}
}

func (t *Tree) Root() []byte {
	return t.rootNode.GetHash()
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
	if t.rootNode.GetChild([]Unit{}) == nil {
		t.rootNode.SetChild(NewLeafNode(unitKey, value, t.rootNode))
		return
	}

	insertNode := t.findNode(unitKey, t.rootNode)
	if insertNode == nil {
		fmt.Println("This should never happen")
	}
	insertNode.Insert(unitKey, value)
}

func (t *Tree) Del(key []byte) bool {
	unitKey := FromBytes(key)

	deleteNode := t.findNode(unitKey, t.rootNode)
	if deleteNode == nil {
		fmt.Println("node does not exist")
		return false
	}

	return deleteNode.Delete(unitKey)
}

func (t *Tree) findNode(key []Unit, node Node) Node {

	if node == nil {
		return nil
	}

	switch node.(type) {
	case *EmptyNode:
		return node
	case *LeafNode:
		return node
	}

	return t.findNode(key, node.GetChild(key))
}

func (t *Tree) PrintTree() {
	t.rootNode.Print()
}
