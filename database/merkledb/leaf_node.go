package merkledb

import (
	"bytes"
	"fmt"
)

// LeafNode is a representation of a Node
// it holds key, LeafValue and a Parent pointer
type LeafNode struct {
	LeafKey            Key    `serialize:"true"`
	LeafValue          []byte `serialize:"true"`
	StoredHash         []byte `serialize:"true"`
	Refs               int32  `serialize:"true"`
	pivotNode          *Pivot
	previousStoredHash []byte
	parent             Node
	persistence        Persistence
}

// NewLeafNode creates a new Leaf Node
func NewLeafNode(key Key, value []byte, parent Node, persistence Persistence) (Node, error) {
	l := &LeafNode{
		LeafKey:     key,
		LeafValue:   value,
		parent:      parent,
		persistence: persistence,
		Refs:        1,
	}

	return l, l.Hash(nil, nil)
}

func (l *LeafNode) GetChild(key Key) (Node, error) {
	return l, nil
}

// Insert in the a LeafNode means that it's either
// the same key - we update the LeafValue
// otherwise - request the Parent to insert the k/v
func (l *LeafNode) Insert(key Key, value []byte) error {
	// only the LeafValue changed - rehash + request the rehash upwards
	if key.Equals(l.LeafKey) {
		if bytes.Equal(l.LeafValue, value) {
			return nil
		}
		l.LeafValue = value

		err := l.Hash(nil, nil)
		if err != nil {
			return err
		}

		l.parent.PivotPoint().Copy(l.PivotPoint())
		return l.parent.Hash(key, l.StoredHash)
	}

	// when it arrives here it hasn't checked this node for the pivot
	if l.Refs > 1 {
		l.PivotPoint().CheckAndSet(l.GetHash(), l.Refs)

		if bytes.Equal(l.PivotPoint().hash, l.GetHash()) {
			l.PivotPoint().passed = true
		}
	}
	l.parent.PivotPoint().Copy(l.PivotPoint())
	// it's actually a new LeafNode
	return l.parent.Insert(key, value)
}

// GetNextNode returns itself
func (l *LeafNode) GetNextNode(prefix Key, start Key, key Key) (Node, error) {
	return l, nil
}

// Delete removes this LeafNode from the Parent
func (l *LeafNode) Delete(key Key) error {
	err := l.persistence.DeleteNode(l)
	if err != nil {
		return err
	}
	l.parent.PivotPoint().Copy(l.pivotNode)
	return l.parent.Delete(key)
}

// SetChild should never be called
func (l *LeafNode) SetChild(node Node) error { return nil }

// SetParent is used on specially when updating the tree ( skipping branchNodes)
func (l *LeafNode) SetParent(node Node) {
	l.parent = node
}

// SetPersistence force sets the persistence in the LeafNode
func (l *LeafNode) SetPersistence(persistence Persistence) {
	l.persistence = persistence
}

// Value returns the stored LeafValue
func (l *LeafNode) Value() []byte {
	return l.LeafValue
}

func (l *LeafNode) Hash(key Key, hash []byte) error {
	newHash := Hash(l.LeafValue, l.LeafKey.ToExpandedBytes())
	if bytes.Equal(l.StoredHash, newHash) {
		return nil
	}

	l.previousStoredHash = l.StoredHash
	l.StoredHash = newHash

	// Hashing creates a new Node - since it has a parent (bc of call stack) we mark it with 1 Reference
	l.Refs = 1
	return l.persistence.StoreNode(l, false)
}

// GetHash returns the StoredHash
func (l *LeafNode) GetHash() []byte {
	return l.StoredHash
}

// GetPreviousHash returns the previousStoredHash
// for deleting unused LeafNode from the DB
func (l *LeafNode) GetPreviousHash() []byte {
	return l.previousStoredHash
}

func (l *LeafNode) References(change int32) int32 {
	l.Refs += change
	return l.Refs
}

func (l *LeafNode) PivotPoint() *Pivot {
	if l.pivotNode == nil {
		l.pivotNode = NewPivot()
	}
	return l.pivotNode
}

// Key returns the stored key
func (l *LeafNode) Key() Key {
	return l.LeafKey
}

// GetChildrenHashes will always return nil
func (l *LeafNode) GetChildrenHashes() [][]byte {
	return nil
}

func (l *LeafNode) GetReHash() []byte {
	return Hash(l.LeafValue, l.LeafKey.ToExpandedBytes())
}

// Clear deletes all nodes attached to this BranchNode
func (l *LeafNode) Clear() error {
	return l.persistence.DeleteNode(l)
}

// Print prints this Node data
func (l *LeafNode) Print(level int32) {
	tabs := ""
	for i := int32(0); i < level; i++ {
		tabs += fmt.Sprint("\t")
	}

	fmt.Printf(tabs + fmt.Sprintf("Leaf ID: %x - Refs: %d - \n%s\tKey: %v \n%s\tVal: %v\n", l.GetHash(), l.Refs, tabs, l.LeafKey, tabs, l.LeafValue))
}

// String converts the node in a string format
func (l *LeafNode) String() string {
	return fmt.Sprintf("Leaf ID: %x - Refs: %d - \n\t\t\t\tKey: %v \n\t\t\t\tVal: %v\n", l.GetHash(), l.Refs, l.LeafKey, l.LeafValue)
}
