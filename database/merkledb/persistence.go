package merkledb

import (
	"encoding/json"

	"github.com/ava-labs/avalanchego/database"
)

// Persistence is the singleton to access data
var Persistence PersistenceData

// PersistenceData holds the DB + the RootNode
type PersistenceData struct {
	db       database.Database
	rootNode Node
}

// GetNodeByUnitKey fetches a Node given a StorageKey
func (p *PersistenceData) GetNodeByUnitKey(storageKey []Unit) (Node, error) {
	if storageKey == nil {
		return p.GetRootNode(), nil
	}

	nodeBytes, err := p.db.Get(ToExpandedBytes(storageKey))
	if err != nil {
		return nil, err
	}

	return convertToNode(nodeBytes)
}

// GetLeafNodeByKey returns a LeafNode given a key
func (p *PersistenceData) GetLeafNodeByKey(key []Unit) (Node, error) {
	if key == nil {
		return nil, database.ErrNotFound
	}

	nodeBytes, err := p.db.Get(ToExpandedBytes(append([]Unit("L-"), key...)))
	if err != nil {
		return nil, err
	}

	return convertToNode(nodeBytes)
}

// GetRootNode returns the RootNode
func (p *PersistenceData) GetRootNode() Node {
	return p.rootNode

}

// StoreNode stores a in the DB Node using its StorageKey
func (p *PersistenceData) StoreNode(n Node) error {
	switch n.(type) {
	case *RootNode:
		p.rootNode = n
	default:
		nBytes, err := json.Marshal(n)
		if err != nil {
			return err
		}
		err = p.db.Put(ToExpandedBytes(n.StorageKey()), nBytes)
		if err != nil {
			return err
		}
	}

	return nil
}

// DeleteNode deletes a node using its StorageKey
func (p *PersistenceData) DeleteNode(n Node) error {
	switch n.(type) {
	case *RootNode:
		panic("No Need to delete rootNode")
	default:
		err := p.db.Delete(ToExpandedBytes(n.StorageKey()))
		if err != nil {
			return err
		}
	}
	return nil
}

// NewPersistence creates a new Persistence
func NewPersistence(db database.Database) {

	// TODO Review it if needs to be async safe
	// lock.Lock()
	// defer lock.Unlock()

	Persistence.db = db
	Persistence.rootNode = NewRootNode()
}

func convertToNode(nodeBytes []byte) (Node, error) {
	var objMap map[string]*json.RawMessage
	err := json.Unmarshal(nodeBytes, &objMap)
	if err != nil {
		return nil, err
	}

	var nodeType string
	err = json.Unmarshal(*objMap["type"], &nodeType)
	if err != nil {
		return nil, err
	}

	var node Node

	switch nodeType {
	case "BranchNode":
		var branchNode BranchNode
		err = json.Unmarshal(nodeBytes, &branchNode)
		if err != nil {
			return nil, err
		}
		node = &branchNode
	case "LeafNode":
		var leafNode LeafNode
		err = json.Unmarshal(nodeBytes, &leafNode)
		if err != nil {
			return nil, err
		}
		node = &leafNode
	}

	return node, nil
}
