package merkledb

import (
	"fmt"

	"github.com/ava-labs/avalanchego/codec/linearcodec"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
)

// Persistence holds the DB + the RootNode
type Persistence struct {
	db          database.Database
	dataChange  map[string][]byte
	rootNodes   []Node
	codec       codec.Manager
	currentRoot int
}

// NewPersistence creates a new Persistence
func NewPersistence(db database.Database) (*Persistence, error) {
	c := linearcodec.NewDefault()
	_ = c.RegisterType(&BranchNode{})
	_ = c.RegisterType(&LeafNode{})
	codecManager := codec.NewDefaultManager()
	if err := codecManager.RegisterCodec(0, c); err != nil {
		return nil, err
	}

	persistence := Persistence{
		db:          db,
		codec:       codecManager,
		currentRoot: 0,
	}

	persistence.rootNodes = append([]Node{}, NewRootNode(&persistence))

	return &persistence, nil
}

// GetNodeByUnitKey fetches a Node given a StorageKey
func (p *Persistence) GetNodeByHash(nodeHash []byte) (Node, error) {
	if nodeHash == nil {
		return p.GetRootNode(), nil
	}

	nodeBytes, err := p.db.Get(nodeHash)
	if err != nil {
		return nil, err
	}

	var node Node
	_, err = p.codec.Unmarshal(nodeBytes, &node)
	node.SetPersistence(p)

	return node, err
}

// GetRootNode returns the RootNode
func (p *Persistence) GetRootNode() Node {
	return p.rootNodes[p.currentRoot]

}

// StoreNode stores a in the DB Node using its StorageKey
func (p *Persistence) StoreNode(n Node) error {
	switch n.(type) {
	case *RootNode:
		p.rootNodes[p.currentRoot] = n
	default:
		nBytes, err := p.codec.Marshal(0, &n)
		if err != nil {
			return err
		}

		err = p.db.Put(n.GetHash(), nBytes)
		if err != nil {
			return err
		}

		previousRefs := n.References(-1)
		n.References(2)

		previousID := n.GetPreviousHash()
		if len(previousID) != 0 && previousRefs == 0 {
			err = p.db.Delete(n.GetPreviousHash())
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// DeleteNode deletes a node using its StorageKey
func (p *Persistence) DeleteNode(n Node) error {
	switch n.(type) {
	case *RootNode:
		return fmt.Errorf("rootNode should not be deleted")
	default:
		if n.References(-1) == 0 {
			err := p.db.Delete(n.GetHash())
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Persistence) SelectRoot(treeRoot int) error {

	nodesLen := len(p.rootNodes)

	if nodesLen > treeRoot {
		p.currentRoot = treeRoot
		return nil
	}
	if nodesLen < treeRoot {
		return fmt.Errorf("NO")
	}
	if nodesLen == treeRoot {
		newRoot := NewRootNode(p)
		newRoot.(*RootNode).child = p.rootNodes[treeRoot-1].(*RootNode).child

		p.currentRoot = treeRoot
		p.rootNodes = append(p.rootNodes, newRoot)
	}

	return nil
}

func (p *Persistence) PrintDB() {
	iterator := p.db.NewIterator()
	for iterator.Next() {
		fmt.Printf("k: %x \n", iterator.Key())
	}
}
