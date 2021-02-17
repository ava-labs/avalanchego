package indexer

import (
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/triggers"
)

const (
	indexName = "index"
)

// Indexer causes accepted containers for a given chain
// to be indexed by their ID and by the order in which
// they were accepted by this node
type Indexer interface {
	IndexChain(chainID ids.ID) error
	StopIndexingChain(chainID ids.ID) error
}

// indexer implements Indexer
type indexer struct {
	lock sync.Mutex

	db database.Database

	// Chain ID --> Index for that chain
	indexedChains map[ids.ID]Index

	// Indices are hooked up to the event dispatcher,
	// which notifies them of accepted containers
	eventDispatcher *triggers.EventDispatcher
}

// NewIndexer returns a new Indexer that uses [db] for persistence.
// Indices learn about newly accepted containers through [eventDispatcher].
func NewIndexer(
	db database.Database,
	eventDispatcher *triggers.EventDispatcher,
) Indexer {
	return &indexer{
		db:              db,
		eventDispatcher: eventDispatcher,
		indexedChains:   make(map[ids.ID]Index),
	}
}

func (i *indexer) IndexChain(chainID ids.ID) error {
	i.lock.Lock()
	defer i.lock.Unlock()

	indexDb := prefixdb.New(chainID[:], i.db)
	index := newIndex(indexDb)
	err := i.eventDispatcher.RegisterChain(chainID, indexName, index)
	if err != nil {
		return fmt.Errorf("couldn't register index for chain %s with event dispatcher: %w", chainID, err)
	}

	i.indexedChains[chainID] = index
	return nil
}

func (i *indexer) StopIndexingChain(chainID ids.ID) error {
	i.lock.Lock()
	defer i.lock.Unlock()

	index, exists := i.indexedChains[chainID]
	if !exists {
		return nil
	}
	delete(i.indexedChains, chainID)
	err := index.Close()
	if err != nil {
		return fmt.Errorf("error while closing index for chain %s: %w", chainID, err)
	}
	return nil
}
