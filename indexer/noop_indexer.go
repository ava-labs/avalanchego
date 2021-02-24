package indexer

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

var (
	errNoOp         = errors.New("this is a no-op indexer")
	_       Indexer = &noOpIndexer{}
)

// NewNoOpIndexer returns a new Indexer that does nothing
func NewNoOpIndexer() Indexer { return &noOpIndexer{} }

// noOpIndexer does nothing
type noOpIndexer struct{}

func (ni *noOpIndexer) IndexChain(chainID ids.ID) error {
	return nil
}

func (ni *noOpIndexer) GetIndex(chainID ids.ID, containerID ids.ID) (uint64, error) {
	return 0, errNoOp
}

func (ni *noOpIndexer) StopIndexingChain(chainID ids.ID) error {
	return nil
}

// GetContainersByIndex ...
func (ni *noOpIndexer) GetContainerByIndex(chainID ids.ID, indexToFetch uint64) (Container, error) {
	return Container{}, errNoOp
}

// GetContainersByIndex ...
func (ni *noOpIndexer) GetContainerRange(chainID ids.ID, startIndex, numToFetch uint64) ([]Container, error) {
	return nil, errNoOp
}

func (ni *noOpIndexer) GetLastAccepted(chainID ids.ID) (Container, error) {
	return Container{}, errNoOp
}

func (ni *noOpIndexer) Handler() (*common.HTTPHandler, error) {
	return nil, errNoOp
}

func (ni *noOpIndexer) Close() error {
	return nil
}
