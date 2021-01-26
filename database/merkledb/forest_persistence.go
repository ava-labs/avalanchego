package merkledb

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database/versiondb"

	"github.com/ava-labs/avalanchego/codec/linearcodec"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
)

// ForestPersistence holds the DB + the RootNode
type ForestPersistence struct {
	db         database.Database
	dataChange map[string][]byte
	codec      codec.Manager
}

// NewForestPersistence creates a new Persistence
func NewForestPersistence(db database.Database) (Persistence, error) {
	c := linearcodec.NewDefault()
	_ = c.RegisterType(&BranchNode{})
	_ = c.RegisterType(&LeafNode{})
	_ = c.RegisterType(&RootNode{})
	codecManager := codec.NewDefaultManager()
	if err := codecManager.RegisterCodec(0, c); err != nil {
		return nil, err
	}

	persistence := ForestPersistence{
		db:    versiondb.New(db),
		codec: codecManager,
	}

	return &persistence, nil
}

// GetNodeByUnitKey fetches a Node given a StorageKey
func (fp *ForestPersistence) GetNodeByHash(nodeHash []byte) (Node, error) {
	nodeBytes, err := fp.db.Get(nodeHash)
	if err != nil {
		return nil, err
	}

	var node Node
	_, err = fp.codec.Unmarshal(nodeBytes, &node)
	node.SetPersistence(fp)

	return node, err
}

// GetRootNode returns the RootNode
func (fp *ForestPersistence) GetRootNode(rootNodeID uint32) (Node, error) {

	node, err := fp.GetNodeByHash(genRootNodeID(rootNodeID))
	if err != nil {
		return nil, err
	}

	return node, nil
}

func (fp *ForestPersistence) NewRoot(rootNodeID uint32) (Node, error) {

	_, err := fp.GetRootNode(rootNodeID)
	if err == database.ErrNotFound {
		newRoot := NewRootNode(rootNodeID, fp)

		err = fp.StoreNode(newRoot)
		if err != nil {
			return nil, err
		}

		return newRoot, nil
	} else if err != nil {
		return nil, err
	}

	return nil, fmt.Errorf("root already exists")
}

func (fp *ForestPersistence) DuplicateRoot(oldRootID uint32, newRootID uint32) (Node, error) {

	oldRoot, err := fp.GetRootNode(oldRootID)
	if err != nil {
		return nil, err
	}

	newRoot, err := fp.NewRoot(newRootID)
	if err != nil {
		return nil, err
	}

	newRoot.(*RootNode).Child = oldRoot.(*RootNode).Child

	return newRoot, nil
}

// StoreNode stores a in the DB Node using its StorageKey
func (fp *ForestPersistence) StoreNode(n Node) error {
	nBytes, err := fp.codec.Marshal(0, &n)
	if err != nil {
		return err
	}

	switch node := n.(type) {
	case *RootNode:
		err = fp.db.Put(genRootNodeID(node.RootID), nBytes)
		if err != nil {
			return err
		}
		return nil
	default:
		err = fp.db.Put(n.GetHash(), nBytes)
		if err != nil {
			return err
		}

		previousRefs := n.References(-1)
		n.References(2)

		previousID := n.GetPreviousHash()
		if len(previousID) != 0 && previousRefs == 0 {
			err = fp.db.Delete(n.GetPreviousHash())
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// DeleteNode deletes a node using its StorageKey
func (fp *ForestPersistence) DeleteNode(n Node) error {
	var hash []byte
	switch node := n.(type) {
	case *RootNode:
		hash = genRootNodeID(node.RootID)
	default:
		hash = n.GetHash()
	}

	if n.References(-1) == 0 {
		err := fp.db.Delete(hash)
		if err != nil {
			return err
		}
	}
	return nil
}

// Commit commits any pending nodes
func (fp *ForestPersistence) Commit(err error) error {
	if err != nil {
		fp.db.(*versiondb.Database).Abort()
		return err
	}
	return fp.db.(*versiondb.Database).Commit()
}

func (fp *ForestPersistence) GetDatabase() database.Database {
	return fp.db
}

func (fp *ForestPersistence) PrintDB() {
	iterator := fp.db.NewIterator()
	for iterator.Next() {
		fmt.Printf("k: %x \n", iterator.Key())
	}
}
