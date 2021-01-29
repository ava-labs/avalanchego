package merkledb

import (
	"bytes"
	"fmt"

	"github.com/ava-labs/avalanchego/database/versiondb"

	"github.com/ava-labs/avalanchego/codec/linearcodec"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
)

// ForestPersistence holds the DB + the RootNode
type ForestPersistence struct {
	db    database.Database
	codec codec.Manager
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
	return fp.GetNodeByHash(genRootNodeID(rootNodeID))
}

// NewRoot creates a new Root and returns it - fails it it already exists
func (fp *ForestPersistence) NewRoot(rootNodeID uint32) (Node, error) {

	_, err := fp.GetRootNode(rootNodeID)
	if err == database.ErrNotFound {
		newRoot := NewRootNode(rootNodeID, fp)
		return newRoot, nil
	} else if err != nil {
		return nil, err
	}

	return nil, fmt.Errorf("root already exists")
}

// DuplicateRoot creates a new RootNode setting its child to point at the oldRootID
func (fp *ForestPersistence) DuplicateRoot(oldRootID uint32, newRootID uint32) (Node, error) {

	oldRoot, err := fp.GetRootNode(oldRootID)
	if err != nil {
		return nil, err
	}

	newRoot, err := fp.NewRoot(newRootID)
	if err != nil {
		return nil, err
	}

	oldRootChild, err := oldRoot.GetChild(Key{})
	if err != nil {
		return nil, err
	}

	oldRootChild.References(1)

	err = newRoot.SetChild(oldRootChild)
	if err != nil {
		return nil, err
	}

	err = fp.StoreNode(oldRootChild)
	if err != nil {
		return nil, err
	}

	err = fp.StoreNode(newRoot)
	if err != nil {
		return nil, err
	}

	return newRoot, nil
}

// StoreNode stores a in the DB Node using its StorageKey
func (fp *ForestPersistence) StoreNode(n Node) error {

	// before storing lets take care of the leftover nodes
	err := fp.ensureRefCounting(n)
	if err != nil {
		return err
	}

	return fp.storeNode(n)
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
		return fp.db.Delete(hash)
	}

	return fp.StoreNode(n)
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

func (fp *ForestPersistence) storeNode(n Node) error {
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
	}

	return nil
}

func (fp *ForestPersistence) ensureRefCounting(n Node) error {
	previousID := n.GetPreviousHash()
	if len(previousID) != 0 {

		prevNode, err := fp.GetNodeByHash(previousID)
		if err != nil {
			return err
		}

		// remove the new node reference from the previous node
		// use the new node refs as a baseline - it may have the refs of a parent with more references
		prevNode.References(n.References(0) - 1)

		// increase the references of children that are shared between the Previous and the New Node
		for _, prevChildHash := range prevNode.GetChildrenHashes() {
			for _, newChildHash := range n.GetChildrenHashes() {

				// they are the same, add a new Reference
				if bytes.Equal(prevChildHash, newChildHash) {
					prevChildNode, err := fp.GetNodeByHash(prevChildHash)
					if err != nil {
						return err
					}

					prevChildNode.References(1)
					err = fp.storeNode(prevChildNode)
					if err != nil {
						return err
					}
				}
			}
		}

		err = fp.storeNode(prevNode)
		if err != nil {
			return err
		}
	}
	return nil
}
