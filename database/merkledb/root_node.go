package merkledb

import "fmt"

// RootNode is the top most node of the tree
type RootNode struct {
	child []Unit
	hash  []byte
}

// NewRootNode returns a RootNode without children
func NewRootNode() Node {
	return &RootNode{}
}

// GetChild returns the child
func (r *RootNode) GetChild(key []Unit) (Node, error) {
	if r.child == nil {
		return nil, nil
	}
	return Persistence.GetNodeByUnitKey(r.child)
}

// GetNextNode returns the child
func (r *RootNode) GetNextNode(prefix []Unit, start []Unit, key []Unit) (Node, error) {
	return Persistence.GetNodeByUnitKey(r.child)
}

// Insert in the RootNode means the branch/leaf needs to group in a new branch
func (r *RootNode) Insert(key []Unit, value []byte) error {
	newBranch := NewBranchNode(SharedPrefix(FromStorageKey(r.child), key), r)

	child, err := Persistence.GetNodeByUnitKey(r.child)
	if err != nil {
		return err
	}

	err = newBranch.SetChild(child)
	if err != nil {
		return err
	}

	r.child = newBranch.StorageKey()

	err = Persistence.StoreNode(r)
	if err != nil {
		return err
	}

	// Order matters insert after setting child
	return newBranch.Insert(key, value)
}

// Delete removes the child
func (r *RootNode) Delete(key []Unit) error {
	r.child = nil
	return nil
}

// SetChild sets the RootNode child to the Node
func (r *RootNode) SetChild(node Node) error {
	r.child = node.StorageKey()
	err := node.SetParent(r)
	if err != nil {
		return err
	}

	r.hash = node.GetHash()
	return Persistence.StoreNode(r)
}

// SetParent should never be reached
func (r *RootNode) SetParent(node Node) error { return nil }

// Value should never be reached
func (r *RootNode) Value() []byte { return nil }

func (r *RootNode) Hash(key []Unit, hash []byte) error {
	if r.child == nil {
		r.hash = nil
	}
	r.hash = hash

	return nil
}

func (r *RootNode) GetHash() []byte {
	return r.hash
}

// Key should never be reached
func (r *RootNode) Key() []Unit { return nil }

// Key should never be reached
func (r *RootNode) StorageKey() []Unit { return nil }

// Print prints the child and requests the child to print itself
func (r *RootNode) Print() {
	fmt.Printf("Root ID: %v - Child: %v \n", r.StorageKey(), r.child)
	if r.child != nil {
		child, err := Persistence.GetNodeByUnitKey(r.child)
		if err != nil {
			panic(err)
		}
		child.Print()
	}
}
