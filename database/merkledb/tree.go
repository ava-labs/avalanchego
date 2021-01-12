package merkledb

import (
	"bytes"
	"fmt"

	"github.com/ava-labs/avalanchego/database/memdb"

	"github.com/ava-labs/avalanchego/database"
)

// Tree holds the tree data
type Tree struct {
	closed bool
}

// Has returns whether the key exists in the tree
func (t *Tree) Has(key []byte) (bool, error) {
	if t.isClosed() != nil {
		return false, t.isClosed()
	}

	node := t.findNode(FromBytes(key), Persistence.GetRootNode())
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
	t.closed = true
	return nil
}

// NewMemoryTree returns a new instance of the Tree with a in-memoryDB
func NewMemoryTree() *Tree {
	return NewTree(memdb.New())
}

// NewTree returns a new instance of the Tree
func NewTree(db database.Database) *Tree {
	NewPersistence(db)
	return &Tree{
		closed: false,
	}
}

func (t *Tree) Root() []byte {
	return Persistence.GetRootNode().GetHash()
}

func (t *Tree) Get(key []byte) ([]byte, error) {
	if t.isClosed() != nil {
		return nil, t.isClosed()
	}

	node := t.findNode(FromBytes(key), Persistence.GetRootNode())
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
	rootChild, err := Persistence.GetRootNode().GetChild([]Unit{})
	if err != nil {
		return err
	}

	if rootChild == nil {
		newLeafNode, err := NewLeafNode(unitKey, value, Persistence.GetRootNode())
		if err != nil {
			return err
		}
		return Persistence.GetRootNode().SetChild(newLeafNode)
	}

	insertNode := t.findNode(unitKey, Persistence.GetRootNode())
	if insertNode == nil {
		return fmt.Errorf("should never happen - can't insert on a nil node k: %v", unitKey)
	}

	return insertNode.Insert(unitKey, value)
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

	deleteNode := t.findNode(unitKey, Persistence.GetRootNode())
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

	nodeChild, err := node.GetChild(key)
	if err != nil {
		panic(err)
	}

	return t.findNode(key, nodeChild)
}

func (t *Tree) PrintTree() {
	Persistence.GetRootNode().Print()
}

func (t *Tree) fetchNextNode(prefix []Unit, start []Unit, key []Unit, node Node) (Node, error) {
	if node == nil || t.closed {
		return nil, database.ErrClosed
	}

	switch node.(type) {
	case *EmptyNode:
		return nil, nil
	case *LeafNode:
		return node, nil
	}

	nextNode, err := node.GetNextNode(prefix, start, key)
	if err != nil {
		return nil, err
	}
	return t.fetchNextNode(prefix, start, key, nextNode)
}

func (t *Tree) isClosed() error {
	if t.closed {
		return database.ErrClosed
	}
	return nil
}
