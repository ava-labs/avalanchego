package merkledb

// Iterator iterates over a database's key/value pairs in ascending key order.
//
// When it encounters an error any seek will return false and will yield no key/
// value pairs. The error can be queried by calling the Error method. Calling
// Release is still necessary.
//
// An iterator must be released after use, but it is not necessary to read an
// iterator until exhaustion. An iterator is not safe for concurrent use, but it
// is safe to use multiple iterators concurrently.
type Iterator struct {
	node   Node
	tree   *Tree
	start  []byte
	prefix []byte
	err    error
}

// NewIterator creates a binary-alphabetical iterator over the entire
// keyspace contained within the key-value database.
func NewIterator(t *Tree) *Iterator {
	return &Iterator{
		tree: t,
		node: &EmptyNode{},
	}
}

// NewIteratorWithStart creates a binary-alphabetical iterator over a subset
// of database content starting at a particular initial key (or after, if it
// does not exist).
func NewIteratorWithStart(t *Tree, start []byte) *Iterator {
	return &Iterator{
		tree:  t,
		node:  &EmptyNode{},
		start: start,
	}
}

// NewIteratorWithPrefix creates a binary-alphabetical iterator over a
// subset of database content with a particular key prefix.
func NewIteratorWithPrefix(t *Tree, prefix []byte) *Iterator {
	return &Iterator{
		tree:   t,
		node:   &EmptyNode{},
		prefix: prefix,
	}
}

// NewIteratorWithStartAndPrefix creates a binary-alphabetical iterator over
// a subset of database content with a particular key prefix starting at a
// specified key.
func NewIteratorWithStartAndPrefix(t *Tree, start, prefix []byte) *Iterator {
	return &Iterator{
		tree:   t,
		node:   &EmptyNode{},
		start:  start,
		prefix: prefix,
	}
}

// Next moves the iterator to the next key/value pair.
// It returns whether the iterator is exhausted.
func (i *Iterator) Next() bool {
	var rootNode Node
	rootNode, i.err = i.tree.persistence.GetRootNode(0)
	if i.err != nil {
		return false
	}

	i.node, i.err = i.tree.fetchNextNode(BytesToKey(i.prefix), BytesToKey(i.start), i.node.Key(), rootNode)
	return i.node != nil
}

// Error returns any accumulated error. Exhausting all the key/value pairs
// is not considered to be an error.
func (i *Iterator) Error() error {
	return i.err
}

// Key returns the key of the current key/value pair, or nil if done.
func (i *Iterator) Key() []byte {
	if i.node == nil {
		return nil
	}
	if _, ok := i.node.(*EmptyNode); ok {
		return nil
	}
	return i.node.Key().ToBytes()
}

// Value returns the value of the current key/value pair, or nil if done.
func (i *Iterator) Value() []byte {
	if i.node == nil {
		return nil
	}
	if _, ok := i.node.(*EmptyNode); ok {
		return nil
	}

	return i.node.Value()
}

// Release releases associated resources. Release should always succeed and
// can be called multiple times without causing error.
func (i *Iterator) Release() {
	i.node = &EmptyNode{}
}
