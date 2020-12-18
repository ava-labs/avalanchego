package tree

import "fmt"

// RootNode is the top most node of the tree
type RootNode struct {
	child Node
	hash  []byte
}

// NewRootNode returns a RootNode without children
func NewRootNode() Node {
	return &RootNode{}
}

// GetChild returns the child
func (r *RootNode) GetChild(key []Unit) Node {
	return r.child
}

// Insert in the RootNode means the branch/leaf needs to group in a new branch
func (r *RootNode) Insert(key []Unit, value []byte) {
	newBranch := NewBranchNode(SharedPrefix(r.child.Key(), key), r)
	newBranch.SetChild(r.child)
	newBranch.Insert(key, value)
	r.child = newBranch
}

// Delete removes the child
func (r *RootNode) Delete(key []Unit) bool {
	r.child = nil
	return true
}

// SetChild sets the RootNode child to the Node
func (r *RootNode) SetChild(node Node) {
	r.child = node
}

// SetParent should never be reached
func (r *RootNode) SetParent(node Node) {}

// Value should never be reached
func (r *RootNode) Value() []byte { return nil }

func (r *RootNode) Hash() {
	if r.child == nil {
		r.hash = nil
		return
	}
	r.hash = r.child.GetHash()
}

func (r *RootNode) GetHash() []byte {
	return r.hash
}

// Key should never be reached
func (r *RootNode) Key() []Unit { return nil }

// Print prints the child and requests the child to print itself
func (r *RootNode) Print() {
	fmt.Printf("Root ID: %p - Child: %p \n", r, r.child)
	if r.child != nil {
		r.child.Print()
	}
}
