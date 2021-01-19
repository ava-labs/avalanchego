package merkledb

import "fmt"

// RootNode is the top most node of the tree
type RootNode struct {
	Child              []byte `serialize:"true"`
	StoredHash         []byte `serialize:"true"`
	RootID             uint32 `serialize:"true"`
	previousStoredHash []byte
	persistence        *Persistence
}

// NewRootNode returns a RootNode without children
func NewRootNode(id uint32, p *Persistence) Node {
	return &RootNode{RootID: id, persistence: p}
}

// GetChild returns the child
func (r *RootNode) GetChild(key Key) (Node, error) {
	if len(r.Child) == 0 {
		return nil, nil
	}
	node, err := r.persistence.GetNodeByHash(r.Child)
	if err != nil {
		return nil, err
	}
	node.SetParent(r)
	node.SetPersistence(r.persistence)
	return node, nil
}

// GetNextNode returns the child if it respects the nextNode inputs
func (r *RootNode) GetNextNode(prefix Key, start Key, key Key) (Node, error) {
	node, err := r.persistence.GetNodeByHash(r.Child)
	if err != nil {
		return nil, err
	}

	if len(key) != 0 {
		if !Greater(node.Key(), key) || node.Key().Equals(key) {
			return nil, nil
		}
	}

	if len(start) != 0 {
		if !Greater(node.Key(), start) || node.Key().Equals(start) {
			return nil, nil
		}
	}

	if len(prefix) != 0 {
		if !IsPrefixed(prefix, node.Key()) {
			return nil, nil
		}
	}

	return r.persistence.GetNodeByHash(r.Child)
}

// Insert in the RootNode means the branch/leaf needs to group in a new branch
func (r *RootNode) Insert(key Key, value []byte) error {
	child, err := r.persistence.GetNodeByHash(r.Child)
	if err != nil {
		return err
	}

	newBranch := NewBranchNode(SharedPrefix(child.Key(), key), r, r.persistence)

	err = newBranch.SetChild(child)
	if err != nil {
		return err
	}

	child.SetParent(newBranch)

	err = newBranch.Insert(key, value)
	if err != nil {
		return err
	}

	return nil
}

// Delete removes the child
func (r *RootNode) Delete(key Key) error {
	return r.Hash(nil, nil)
}

// SetChild sets the RootNode child to the Node
func (r *RootNode) SetChild(node Node) error {
	node.SetParent(r)

	return r.Hash(nil, node.GetHash())
}

// SetParent should never be reached
func (r *RootNode) SetParent(node Node) {}

// SetPersistence should never be reached
func (r *RootNode) SetPersistence(p *Persistence) {
	r.persistence = p
}

// Value should never be reached
func (r *RootNode) Value() []byte { return nil }

func (r *RootNode) Hash(key Key, hash []byte) error {
	r.Child = hash

	rootIDByte := []byte{
		byte(0xff & r.RootID),
		byte(0xff & (r.RootID >> 8)),
		byte(0xff & (r.RootID >> 16)),
		byte(0xff & (r.RootID >> 24))}

	r.StoredHash = Hash(rootIDByte, r.Child, r.Child)

	return r.persistence.StoreNode(r)
}

func (r *RootNode) GetHash() []byte {
	return r.StoredHash
}

func (r *RootNode) GetPreviousHash() []byte {
	return r.Child
}

func (r *RootNode) References(change int32) int32 {
	return 0
}

// Key should never be reached
func (r *RootNode) Key() Key { return nil }

// Print prints the child and requests the child to print itself
func (r *RootNode) Print() {
	fmt.Printf("Root ID: %v - Child: %x \n", r.Key(), r.Child)
	if len(r.Child) != 0 {
		child, err := r.persistence.GetNodeByHash(r.Child)
		if err != nil {
			panic(err)
		}
		child.Print()
	}
}

func rootNodeID(rootNodeID uint32) []byte {
	rootIDByte := []byte{
		byte(0xff & rootNodeID),
		byte(0xff & (rootNodeID >> 8)),
		byte(0xff & (rootNodeID >> 16)),
		byte(0xff & (rootNodeID >> 24))}

	return Hash(rootIDByte, []byte{'r', 'o', 'o', 't'})
}
