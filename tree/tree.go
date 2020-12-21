package tree

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
)

// Tree holds the tree data
type Tree struct {
	rootNode Node
}

func (t *Tree) Has(key []byte) (bool, error) {
	node := t.findNode(FromBytes(key), t.rootNode)
	if node == nil {
		return false, nil
	}
	return true, nil
}

func (t *Tree) NewBatch() database.Batch {
	panic("implement me")
}

// NewIterator creates a binary-alphabetical iterator over the entire
// keyspace contained within the key-value database.
func (t *Tree) NewIterator() database.Iterator {
	return NewIterator(t)
}

// NewIteratorWithStart creates a binary-alphabetical iterator over a subset
// of database content starting at a particular initial key (or after, if it
// does not exist).
func (t *Tree) NewIteratorWithStart(start []byte) database.Iterator {
	return NewIteratorWithStart(start)
}

// NewIteratorWithPrefix creates a binary-alphabetical iterator over a
// subset of database content with a particular key prefix.
func (t *Tree) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return NewIteratorWithPrefix(prefix)
}

// NewIteratorWithStartAndPrefix creates a binary-alphabetical iterator over
// a subset of database content with a particular key prefix starting at a
// specified key.
func (t *Tree) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	return NewIteratorWithStartAndPrefix(start, prefix)
}

func (t *Tree) Stat(property string) (string, error) {
	return "nil", nil
}

func (t *Tree) Compact(start []byte, limit []byte) error {
	return nil
}

func (t *Tree) Close() error {
	return nil
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

// Put travels the tree and finds the node to insert the LeafNode
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

// Delete is fills the implementation
func (t *Tree) Delete(key []byte) error {
	t.Del(key)
	return nil
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

func (t *Tree) fetchNextNode(key []Unit, node Node) Node {
	if node == nil {
		return nil
	}

	switch node.(type) {
	case *EmptyNode:
		return nil
	case *LeafNode:
		return node
	}

	return t.fetchNextNode(key, node.GetNextNode(key))
}
