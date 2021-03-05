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

func (ni *noOpIndexer) IndexChain(_ ids.ID) error {
	return nil
}

func (ni *noOpIndexer) GetIndex(_ ids.ID, _ ids.ID) (uint64, error) {
	return 0, errNoOp
}

func (ni *noOpIndexer) CloseIndex(_ ids.ID) error {
	return nil
}

// GetContainersByIndex ...
func (ni *noOpIndexer) GetContainerByIndex(_ ids.ID, _ uint64) (Container, error) {
	return Container{}, errNoOp
}

// GetContainersByIndex ...
func (ni *noOpIndexer) GetContainerRange(_ ids.ID, _, _ uint64) ([]Container, error) {
	return nil, errNoOp
}

func (ni *noOpIndexer) GetLastAccepted(_ ids.ID) (Container, error) {
	return Container{}, errNoOp
}

func (ni *noOpIndexer) Handler() (*common.HTTPHandler, error) {
	return nil, errNoOp
}

func (ni *noOpIndexer) Close() error {
	return nil
}

func (ni *noOpIndexer) GetContainerByID(_, _ ids.ID) (Container, error) {
	return Container{}, errNoOp
}

func (ni *noOpIndexer) GetIndexedChains() []ids.ID {
	return nil
}
