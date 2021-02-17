package indexer

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
)

// Index ... TODO add comment
// Index implements triggers.Acceptor
type Index interface {
	Accept(ctx *snow.Context, containerID ids.ID, container []byte) error
	GetContainerIDByIndex(index uint64) (ids.ID, error)
	GetContainersIDsByRange(startIndex uint64, numToFetch uint64) ([]ids.ID, error)
	GetContainerByIndex(index uint64) ([]byte, error)
	GetContainrsByRange(startIndex uint64, numToFetch uint64) ([][]byte, error)
	Close() error
}

// newIndex returns a new index.
// TODO implement
func newIndex(db database.Database) Index {
	return nil
}
