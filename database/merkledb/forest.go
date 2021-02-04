package merkledb

import "github.com/ava-labs/avalanchego/database"

// Forest is the entrypoint of the merkle database for multi-root trees
type Forest struct {
	persistence Persistence
}

// NewForest creates a multi-root merkle radix tree
func NewForest(db database.Database) *Forest {
	persistence, err := NewForestPersistence(db)
	if err != nil {
		panic(err)
	}
	return &Forest{
		persistence: persistence,
	}
}

// CreateEmptyTree creates a new Empty Tree if there isn't one with that treeRootID
func (f *Forest) CreateEmptyTree(treeRootID uint32) (*Tree, error) {

	root, err := f.persistence.NewRoot(treeRootID)
	if err != nil {
		return nil, err
	}

	err = f.persistence.Commit(f.persistence.StoreNode(root, true))
	if err != nil {
		return nil, err
	}

	return NewTreeWithRoot(f.persistence, root), nil
}

// Copy creates a new Tree that points to the child of oldRootID
func (f *Forest) Copy(oldRootID uint32, newRootID uint32) (*Tree, error) {
	root, err := f.persistence.DuplicateRoot(oldRootID, newRootID)
	if err != nil {
		return nil, err
	}

	err = f.persistence.Commit(nil)
	if err != nil {
		return nil, err
	}

	return NewTreeWithRoot(f.persistence, root), nil
}

// Get returns an existing tree root
func (f *Forest) Get(treeRootID uint32) (*Tree, error) {
	root, err := f.persistence.GetRootNode(treeRootID)
	if err != nil {
		return nil, err
	}
	return NewTreeWithRoot(f.persistence, root), nil
}

// Delete deletes all elements in the given Tree
func (f *Forest) Delete(treeRootID uint32) error {
	tree, err := f.Get(treeRootID)
	if err != nil {
		return err
	}

	return f.persistence.Commit(tree.rootNode.Clear())
}
