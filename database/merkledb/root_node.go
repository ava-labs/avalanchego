package merkledb

import "fmt"

// RootNode is the top most node of the tree
type RootNode struct {
	Child              []byte `serialize:"true"`
	StoredHash         []byte `serialize:"true"`
	RootID             uint32 `serialize:"true"`
	previousStoredHash []byte
	persistence        Persistence
}

// NewRootNode returns a RootNode without children
func NewRootNode(id uint32, p Persistence) Node {
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
	return node, nil
}

// GetNextNode returns the child if it respects the nextNode inputs
// Instead of checking all the positive condition
// the single child Node will only be returned if it does not fail any of the negative condition
// ie. if there's a prefix it will fail/return nil, if the child Node key does not respect that condition
func (r *RootNode) GetNextNode(prefix Key, start Key, key Key) (Node, error) {
	node, err := r.persistence.GetNodeByHash(r.Child)
	if err != nil {
		return nil, err
	}

	nodeKey := node.Key()

	if len(key) != 0 {
		if !Greater(nodeKey, key) || nodeKey.Equals(key) {
			return nil, nil
		}
	}

	if len(start) != 0 {
		if !Greater(nodeKey, start) || nodeKey.Equals(start) {
			return nil, nil
		}
	}

	if len(prefix) != 0 {
		if !prefix.ContainsPrefix(nodeKey) {
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

	return newBranch.Insert(key, value)
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
func (r *RootNode) SetPersistence(p Persistence) {
	r.persistence = p
}

// Value should never be reached
func (r *RootNode) Value() []byte { return nil }

func (r *RootNode) Hash(key Key, hash []byte) error {
	r.Child = hash
	r.StoredHash = Hash(genRootNodeID(r.RootID), r.Child)

	if len(hash) == 0 {
		r.StoredHash = nil
		return r.persistence.DeleteNode(r)
	}

	return r.persistence.StoreNode(r)
}

func (r *RootNode) GetHash() []byte {
	return r.StoredHash
}

func (r *RootNode) GetPreviousHash() []byte {
	return nil
}

func (r *RootNode) References(change int32) int32 {
	return 0
}

// Key should never be reached
func (r *RootNode) Key() Key { return nil }

// GetChildrenHashes will always return nil
func (r *RootNode) GetChildrenHashes() [][]byte {
	return [][]byte{r.Child}
}

// GetReHash should never be called
func (r *RootNode) GetReHash() []byte {
	return Hash(genRootNodeID(r.RootID), r.Child)
}

// Clear deletes all nodes attached to this root
func (r *RootNode) Clear() error {
	if len(r.Child) == 0 {
		return r.persistence.DeleteNode(r)
	}

	child, err := r.persistence.GetNodeByHash(r.Child)
	if err != nil {
		return err
	}
	err = child.Clear()
	if err != nil {
		return err
	}

	return r.persistence.DeleteNode(r)
}

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

// String converts the node in a string format
func (r *RootNode) String() string {
	return fmt.Sprintf("Root ID: %v - Child: %x \n", r.Key(), r.Child)
}

func genRootNodeID(rootNodeID uint32) []byte {
	rootIDByte := []byte{
		byte(rootNodeID),
		byte(rootNodeID >> 8),
		byte(rootNodeID >> 16),
		byte(rootNodeID >> 24)}

	return Hash(rootIDByte, []byte{'r', 'o', 'o', 't'})
}
