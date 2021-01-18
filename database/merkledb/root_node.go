package merkledb

import "fmt"

// RootNode is the top most node of the tree
type RootNode struct {
	child       []byte
	persistence *Persistence
}

// NewRootNode returns a RootNode without children
func NewRootNode(p *Persistence) Node {
	return &RootNode{persistence: p}
}

// GetChild returns the child
func (r *RootNode) GetChild(key Key) (Node, error) {
	if r.child == nil {
		return nil, nil
	}
	node, err := r.persistence.GetNodeByHash(r.child)
	if err != nil {
		return nil, err
	}
	node.SetParent(r)
	node.SetPersistence(r.persistence)
	return node, nil
}

// GetNextNode returns the child
func (r *RootNode) GetNextNode(prefix Key, start Key, key Key) (Node, error) {
	return r.persistence.GetNodeByHash(r.child)
}

// Insert in the RootNode means the branch/leaf needs to group in a new branch
func (r *RootNode) Insert(key Key, value []byte) error {
	child, err := r.persistence.GetNodeByHash(r.child)
	if err != nil {
		return err
	}

	newBranch := NewBranchNode(SharedPrefix(child.Key(), key), r, r.persistence)

	err = newBranch.SetChild(child)
	if err != nil {
		return err
	}

	err = newBranch.Insert(key, value)
	if err != nil {
		return err
	}

	child.SetParent(newBranch)
	return nil
}

// Delete removes the child
func (r *RootNode) Delete(key Key) error {
	r.child = nil
	return nil
}

// SetChild sets the RootNode child to the Node
func (r *RootNode) SetChild(node Node) error {
	r.child = node.GetHash()
	node.SetParent(r)

	return r.persistence.StoreNode(r)
}

// SetParent should never be reached
func (r *RootNode) SetParent(node Node) {}

// SetPersistence should never be reached
func (r *RootNode) SetPersistence(p *Persistence) {}

// Value should never be reached
func (r *RootNode) Value() []byte { return nil }

func (r *RootNode) Hash(key Key, hash []byte) error {
	r.child = hash

	return r.persistence.StoreNode(r)
}

func (r *RootNode) GetHash() []byte {
	return r.child
}

func (r *RootNode) GetPreviousHash() []byte {
	return r.child
}

func (r *RootNode) References(change int32) int32 {
	return 0
}

// Key should never be reached
func (r *RootNode) Key() Key { return nil }

// Print prints the child and requests the child to print itself
func (r *RootNode) Print() {
	fmt.Printf("Root ID: %v - Child: %x \n", r.Key(), r.child)
	if len(r.child) != 0 {
		child, err := r.persistence.GetNodeByHash(r.child)
		if err != nil {
			panic(err)
		}
		child.Print()
	}
}
