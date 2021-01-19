package merkledb

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database/versiondb"

	"github.com/ava-labs/avalanchego/codec/linearcodec"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
)

// Persistence holds the DB + the RootNode
type Persistence struct {
	db          database.Database
	dataChange  map[string][]byte
	codec       codec.Manager
	currentRoot uint32
}

// NewPersistence creates a new Persistence
func NewPersistence(db database.Database) (*Persistence, error) {
	c := linearcodec.NewDefault()
	_ = c.RegisterType(&BranchNode{})
	_ = c.RegisterType(&LeafNode{})
	_ = c.RegisterType(&RootNode{})
	codecManager := codec.NewDefaultManager()
	if err := codecManager.RegisterCodec(0, c); err != nil {
		return nil, err
	}

	persistence := Persistence{
		db:          versiondb.New(db),
		codec:       codecManager,
		currentRoot: 0,
	}

	return &persistence, nil
}

// GetNodeByUnitKey fetches a Node given a StorageKey
func (p *Persistence) GetNodeByHash(nodeHash []byte) (Node, error) {
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
	node, err := p.GetNodeByHash(rootNodeID(p.currentRoot))
	if err != nil {
		if err == database.ErrNotFound {
			newRoot := NewRootNode(p.currentRoot, p)
			if p.currentRoot == 0 {
				return newRoot
			}

			previousRoot, err := p.GetNodeByHash(rootNodeID(p.currentRoot - 1))
			if err != nil {
				panic(err)
			}

			newRoot.(*RootNode).Child = previousRoot.(*RootNode).Child

			return newRoot
		}
		panic(err)
	}

	return node

}

// StoreNode stores a in the DB Node using its StorageKey
func (p *Persistence) StoreNode(n Node) error {
	nBytes, err := p.codec.Marshal(0, &n)
	if err != nil {
		return err
	}

	switch node := n.(type) {
	case *RootNode:
		err = p.db.Put(rootNodeID(node.RootID), nBytes)
		if err != nil {
			return err
		}
		return nil
	default:
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

// Commit commits any pending nodes
func (p *Persistence) Commit(err error) error {
	if err != nil {
		p.db.(*versiondb.Database).Abort()
		return err
	}
	return p.db.(*versiondb.Database).Commit()
}

func (p *Persistence) SelectRoot(treeRoot uint32) {

	p.currentRoot = treeRoot
}

func (p *Persistence) PrintDB() {
	iterator := p.db.NewIterator()
	for iterator.Next() {
		fmt.Printf("k: %x \n", iterator.Key())
	}
}
