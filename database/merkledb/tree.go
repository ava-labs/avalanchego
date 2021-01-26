package merkledb

import (
	"bytes"
	"fmt"

	"github.com/ava-labs/avalanchego/database/versiondb"

	"github.com/ava-labs/avalanchego/database"
)

// Tree holds the tree data
type Tree struct {
	closed      bool
	persistence Persistence
	rootNode    Node
}

// Has returns whether the key exists in the tree
func (t *Tree) Has(key []byte) (bool, error) {
	if t.closed {
		return false, database.ErrClosed
	}

	node, err := t.findNode(BytesToKey(key), t.rootNode)
	if err != nil {
		return false, err
	}
	if node == nil || !bytes.Equal(node.Key().ToBytes(), key) {
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
	return "", database.ErrNotFound
}

func (t *Tree) Compact(start []byte, limit []byte) error {
	if t.closed {
		return database.ErrClosed
	}
	return nil
}

func (t *Tree) Close() error {
	if t.closed {
		return database.ErrClosed
	}

	t.closed = true
	return t.persistence.GetDatabase().(*versiondb.Database).GetDatabase().Close()
}

// NewTree returns a new instance of the Tree
func NewTree(db database.Database) *Tree {
	persistence, err := NewTreePersistence(db)
	if err != nil {
		panic(err)
	}
	rootNode, err := persistence.NewRoot(0)
	if err != nil {
		panic(err)
	}

	return &Tree{
		closed:      false,
		persistence: persistence,
		rootNode:    rootNode,
	}
}

// NewTree returns a new instance of the Tree
func NewTreeWithRoot(persistence Persistence, root Node) *Tree {
	return &Tree{
		closed:      false,
		persistence: persistence,
		rootNode:    root,
	}
}

func (t *Tree) Root() ([]byte, error) {
	return t.rootNode.GetHash(), nil
}

func (t *Tree) Get(key []byte) ([]byte, error) {
	if t.closed {
		return nil, database.ErrClosed
	}

	node, err := t.findNode(BytesToKey(key), t.rootNode)
	if err != nil {
		return nil, err
	}
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
	return t.persistence.Commit(t.put(key, value))
}

func (t *Tree) Delete(key []byte) error {
	return t.persistence.Commit(t.del(key))
}

// findNode traverses the Tree and finds the most suited node for the traversal based on the given key.
// It traverses the tree trying to find the Node where that key might exist but doesn't guarantee
// that the key actually exists, it's the callers responsibility to handle that.
// it will return
// EmptyNode - if a position is found but no node exists
// LeafNode - if there is a K/V pair in that position
func (t *Tree) findNode(key Key, node Node) (Node, error) {

	if node == nil {
		return nil, nil
	}

	switch node.(type) {
	case *EmptyNode:
		return node, nil
	case *LeafNode:
		return node, nil
	}

	nodeChild, err := node.GetChild(key)
	if err != nil {
		return nil, err
	}

	return t.findNode(key, nodeChild)
}

func (t *Tree) PrintTree() {
	t.rootNode.Print()
}

func (t *Tree) fetchNextNode(prefix Key, start Key, key Key, node Node) (Node, error) {
	if node == nil {
		return nil, database.ErrClosed
	}
	if t.closed {
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

func (t *Tree) GetNode(hash []byte) (Node, error) {
	if t.closed {
		return nil, database.ErrClosed
	}

	return t.persistence.GetNodeByHash(hash)
}

// PutRootNode inserts a rootNode
func (t *Tree) PutRootNode(node Node) error {
	if t.closed {
		return database.ErrClosed
	}

	t.rootNode = node
	err := t.persistence.StoreNode(node)
	if err != nil {
		return err
	}

	return nil
}

// PutNodeAndCheck inserts a node in the tree and checks if the hash is correct
func (t *Tree) PutNodeAndCheck(node Node, parentHash []byte) error {
	if t.closed {
		return database.ErrClosed
	}

	if !bytes.Equal(node.GetReHash(), parentHash) {
		return fmt.Errorf("node hash is not the same as the parent Hash")
	}

	err := t.persistence.StoreNode(node)
	if err != nil {
		return err
	}

	return nil
}

func (t *Tree) put(key []byte, value []byte) error {
	if t.closed {
		return database.ErrClosed
	}

	unitKey := BytesToKey(key)

	rootChild, err := t.rootNode.GetChild(Key{})
	if err != nil {
		return err
	}
	if rootChild == nil {
		newLeafNode, err := NewLeafNode(unitKey, value, t.rootNode, t.persistence)
		if err != nil {
			return err
		}

		return t.rootNode.SetChild(newLeafNode)
	}

	insertNode, err := t.findNode(unitKey, t.rootNode)
	if err != nil {
		return err
	}
	if insertNode == nil {
		return fmt.Errorf("should never happen - can't insert on a nil node k: %v", unitKey)
	}

	return insertNode.Insert(unitKey, value)
}

func (t *Tree) del(key []byte) error {
	if t.closed {
		return database.ErrClosed
	}

	unitKey := BytesToKey(key)

	deleteNode, err := t.findNode(unitKey, t.rootNode)
	if err != nil {
		return err
	}
	if deleteNode == nil {
		return nil
	}

	return deleteNode.Delete(unitKey)
}
