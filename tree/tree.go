package tree

import (
	"bytes"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
)

// Tree holds the tree data
type Tree struct {
	rootNode Node
	status   string
}

func (t *Tree) Has(key []byte) (bool, error) {
	if t.isClosed() != nil {
		return false, t.isClosed()
	}

	node := t.findNode(FromBytes(key), t.rootNode)
	if node == nil || !bytes.Equal(ToBytes(node.Key()), key) {
		return false, nil
	}
	return true, nil
}

// NewBatch creates a write-only database that buffers changes to its host db
// until a final write is called.
func (t *Tree) NewBatch() database.Batch {
	return NewBatch(t)
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
	return NewIteratorWithStart(t, start)
}

// NewIteratorWithPrefix creates a binary-alphabetical iterator over a
// subset of database content with a particular key prefix.
func (t *Tree) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return NewIteratorWithPrefix(t, prefix)
}

// NewIteratorWithStartAndPrefix creates a binary-alphabetical iterator over
// a subset of database content with a particular key prefix starting at a
// specified key.
func (t *Tree) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	return NewIteratorWithStartAndPrefix(t, start, prefix)
}

func (t *Tree) Stat(property string) (string, error) {
	return "nil", nil
}

func (t *Tree) Compact(start []byte, limit []byte) error {
	if t.isClosed() != nil {
		return t.isClosed()
	}
	return nil
}

func (t *Tree) Close() error {
	if t.isClosed() != nil {
		return t.isClosed()
	}
	t.status = "closed"
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

func (t *Tree) Get(key []byte) ([]byte, error) {
	if t.isClosed() != nil {
		return nil, t.isClosed()
	}
	node := t.findNode(FromBytes(key), t.rootNode)
	if node == nil {
		return nil, database.ErrNotFound
	}
	if _, ok := node.(*EmptyNode); ok {
		return nil, database.ErrNotFound
	}
	return node.Value(), nil
}

// Put travels the tree and finds the node to insert the LeafNode
func (t *Tree) Put(key []byte, value []byte) error {
	if t.isClosed() != nil {
		return t.isClosed()
	}

	unitKey := FromBytes(key)
	if t.rootNode.GetChild([]Unit{}) == nil {
		t.rootNode.SetChild(NewLeafNode(unitKey, value, t.rootNode))
		return nil
	}

	insertNode := t.findNode(unitKey, t.rootNode)
	if insertNode == nil {
		fmt.Println("This should never happen")
	}
	insertNode.Insert(unitKey, value)
	return nil
}

// Delete is fills the implementation
func (t *Tree) Delete(key []byte) error {
	if t.isClosed() != nil {
		return t.isClosed()
	}

	t.Del(key)
	return nil
}

func (t *Tree) Del(key []byte) bool {
	unitKey := FromBytes(key)

	deleteNode := t.findNode(unitKey, t.rootNode)
	if deleteNode == nil {
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

func (t *Tree) fetchNextNode(prefix []Unit, start []Unit, key []Unit, node Node) (Node, error) {
	if node == nil || t.status == "closed" {
		return nil, database.ErrClosed
	}

	switch node.(type) {
	case *EmptyNode:
		return nil, nil
	case *LeafNode:
		return node, nil
	}

	return t.fetchNextNode(prefix, start, key, node.GetNextNode(prefix, start, key))
}

func (t *Tree) isClosed() error {
	if t.status == "closed" {
		return database.ErrClosed
	}
	return nil
}
