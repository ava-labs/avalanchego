package tree

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
	node Node
	tree *Tree
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
func NewIteratorWithStart(start []byte) *Iterator {
	return &Iterator{}
}

// NewIteratorWithPrefix creates a binary-alphabetical iterator over a
// subset of database content with a particular key prefix.
func NewIteratorWithPrefix(prefix []byte) *Iterator {
	return &Iterator{}
}

// NewIteratorWithStartAndPrefix creates a binary-alphabetical iterator over
// a subset of database content with a particular key prefix starting at a
// specified key.
func NewIteratorWithStartAndPrefix(start, prefix []byte) *Iterator {
	return &Iterator{}
}

// Next moves the iterator to the next key/value pair. It returns whether
// the iterator is exhausted.
func (i *Iterator) Next() bool {
	i.node = i.tree.fetchNextNode(i.node.Key(), i.tree.rootNode)
	return i.node != nil
}

// Error returns any accumulated error. Exhausting all the key/value pairs
// is not considered to be an error.
func (i *Iterator) Error() error {
	panic("implement me")
}

// Key returns the key of the current key/value pair, or nil if done.
func (i *Iterator) Key() []byte {
	return ToBytes(i.node.Key())
}

// Value returns the value of the current key/value pair, or nil if done.
func (i *Iterator) Value() []byte {
	return i.node.Value()
}

// Release releases associated resources. Release should always succeed and
// can be called multiple times without causing error.
func (i *Iterator) Release() {
	panic("implement me")
}
