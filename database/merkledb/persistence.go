package merkledb

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database/memdb"

	"github.com/ava-labs/avalanchego/codec/linearcodec"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
)

// Persistence holds the DB + the RootNode
type Persistence struct {
	cache      database.Database
	db         database.Database
	dataChange map[string][]byte
	rootNode   Node
	codec      codec.Manager
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
		db:    db,
		codec: codecManager,
		cache: memdb.New(),
	}
	persistence.rootNode = NewRootNode(&persistence)

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
	return p.rootNode

}

// StoreNode stores a in the DB Node using its StorageKey
func (p *Persistence) StoreNode(n Node) error {
	switch n.(type) {
	case *RootNode:
		p.rootNode = n
	default:
		nBytes, err := p.codec.Marshal(0, &n)
		if err != nil {
			return err
		}

		err = p.db.Put(n.GetHash(), nBytes)
		if err != nil {
			return err
		}

		previousID := n.GetPreviousHash()
		if len(previousID) != 0 {
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
		err := p.db.Delete(n.GetHash())
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *Persistence) Start() {
}

func (p *Persistence) Commit(err error) error {

	if err != nil {
		return err
	}
	return nil
}

func (p *Persistence) PrintDB() {
	iterator := p.db.NewIterator()
	for iterator.Next() {
		fmt.Printf("k: %x \n", iterator.Key())
	}
}
