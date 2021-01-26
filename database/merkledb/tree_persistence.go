package merkledb

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database/versiondb"

	"github.com/ava-labs/avalanchego/codec/linearcodec"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
)

// TreePersistence holds the DB + the RootNode
type TreePersistence struct {
	db          database.Database
	dataChange  map[string][]byte
	codec       codec.Manager
	currentRoot uint32
}

// NewTreePersistence creates a new TreePersistence
func NewTreePersistence(db database.Database) (Persistence, error) {
	c := linearcodec.NewDefault()
	_ = c.RegisterType(&BranchNode{})
	_ = c.RegisterType(&LeafNode{})
	_ = c.RegisterType(&RootNode{})
	codecManager := codec.NewDefaultManager()
	if err := codecManager.RegisterCodec(0, c); err != nil {
		return nil, err
	}

	TreePersistence := TreePersistence{
		db:          versiondb.New(db),
		codec:       codecManager,
		currentRoot: 0,
	}

	return &TreePersistence, nil
}

// GetNodeByUnitKey fetches a Node given a StorageKey
func (tp *TreePersistence) GetNodeByHash(nodeHash []byte) (Node, error) {
	nodeBytes, err := tp.db.Get(nodeHash)
	if err != nil {
		return nil, err
	}

	var node Node
	_, err = tp.codec.Unmarshal(nodeBytes, &node)
	node.SetPersistence(tp)

	return node, err
}

// GetRootNode returns the RootNode
func (tp *TreePersistence) GetRootNode(rootNodeID uint32) (Node, error) {

	node, err := tp.GetNodeByHash(genRootNodeID(0))
	if err != nil {
		return nil, err
	}

	return node, nil
}

func (tp *TreePersistence) NewRoot(rootNodeID uint32) (Node, error) {
	rootNode, err := tp.GetRootNode(0)
	if err == database.ErrNotFound {
		newRoot := NewRootNode(0, tp)
		return newRoot, nil
	} else if err != nil {
		return nil, err
	}

	return rootNode, nil
}
func (tp *TreePersistence) DuplicateRoot(oldRootID uint32, newRootID uint32) (Node, error) {
	return nil, fmt.Errorf("root can not be duplicated - use a forest instead")
}

// StoreNode stores a in the DB Node using its StorageKey
func (tp *TreePersistence) StoreNode(n Node) error {
	nBytes, err := tp.codec.Marshal(0, &n)
	if err != nil {
		return err
	}

	switch n.(type) {
	case *RootNode:
		err = tp.db.Put(genRootNodeID(0), nBytes)
		if err != nil {
			return err
		}
		return nil
	default:
		err = tp.db.Put(n.GetHash(), nBytes)
		if err != nil {
			return err
		}

		previousID := n.GetPreviousHash()
		if len(previousID) != 0 {
			err = tp.db.Delete(n.GetPreviousHash())
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// DeleteNode deletes a node using its StorageKey
func (tp *TreePersistence) DeleteNode(n Node) error {
	var hash []byte
	switch node := n.(type) {
	case *RootNode:
		hash = genRootNodeID(node.RootID)
	default:
		hash = n.GetHash()
	}

	err := tp.db.Delete(hash)
	if err != nil {
		return err
	}

	return nil
}

// Commit commits any pending nodes
func (tp *TreePersistence) Commit(err error) error {
	if err != nil {
		tp.db.(*versiondb.Database).Abort()
		return err
	}
	return tp.db.(*versiondb.Database).Commit()
}

func (tp *TreePersistence) GetDatabase() database.Database {
	return tp.db
}

func (tp *TreePersistence) PrintDB() {
	iterator := tp.db.NewIterator()
	for iterator.Next() {
		fmt.Printf("k: %x \n", iterator.Key())
	}
}
