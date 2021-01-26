package merkledb

import "github.com/ava-labs/avalanchego/database"

// Persistence holds the DB + the RootNode
type Persistence interface {
	GetNodeByHash(nodeHash []byte) (Node, error)
	GetRootNode(rootNodeID uint32) (Node, error)
	NewRoot(rootNodeID uint32) (Node, error)
	StoreNode(n Node) error
	DeleteNode(n Node) error
	Commit(err error) error
	DuplicateRoot(oldRootID uint32, newRootID uint32) (Node, error)
	GetDatabase() database.Database
}
