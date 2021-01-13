package merkledb

import (
	"encoding/json"

	"github.com/ava-labs/avalanchego/database"
)

var Persistence PersistenceData

type PersistenceData struct {
	db       database.Database
	rootNode Node
}

func (p *PersistenceData) GetNodeByUnitKey(key []Unit) (Node, error) {
	if key == nil {
		return p.GetRootNode(), nil
	}

	nodeBytes, err := p.db.Get(ToExpandedBytes(key))
	if err != nil {
		return nil, err
	}

	return convertToNode(nodeBytes)
}

func (p *PersistenceData) GetLeafNodeByKey(key []Unit) (Node, error) {
	if key == nil {
		return nil, nil
	}

	nodeBytes, err := p.db.Get(ToExpandedBytes(append([]Unit("L-"), key...)))
	if err != nil {
		return nil, err
	}

	return convertToNode(nodeBytes)
}

func (p *PersistenceData) GetRootNode() Node {
	return p.rootNode

}

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
