package indexer

import (
	"fmt"
	"sync"

	cjson "github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/gorilla/rpc/v2"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/triggers"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	indexNamePrefix = "index-"
	codecVersion    = uint16(0)
)

var (
	_ Indexer = &indexer{}
)

// Indexer causes accepted containers for a given chain
// to be indexed by their ID and by the order in which
// they were accepted by this node
type Indexer interface {
	IndexChain(chainID ids.ID) error
	StopIndexingChain(chainID ids.ID) error
	GetContainerByIndex(chainID ids.ID, index uint64) (Container, error)
	GetContainerRange(chainID ids.ID, startIndex, numToFetch uint64) ([]Container, error)
	GetLastAccepted(chainID ids.ID) (Container, error)
	GetIndex(chainID, containerID ids.ID) (uint64, error)
	GetContainerByID(chainID, containerID ids.ID) (Container, error)
	// Handler returns a handler that handles incoming API calls
	Handler() (*common.HTTPHandler, error)
	Close() error
}

// NewIndexer returns a new Indexer that uses [db] for persistence.
// Indices learn about newly accepted containers through [eventDispatcher].
func NewIndexer(
	name string,
	db database.Database,
	log logging.Logger,
	eventDispatcher *triggers.EventDispatcher,
	initiallyIndexedChains []ids.ID,
	chainLookup func(string) (ids.ID, error),
) (Indexer, error) {
	indexer := &indexer{
		name:            name,
		codec:           codec.NewDefaultManager(),
		log:             log,
		db:              db,
		eventDispatcher: eventDispatcher,
		indexedChains:   make(map[ids.ID]Index),
		chainLookup:     chainLookup,
	}
	err := indexer.codec.RegisterCodec(codecVersion, linearcodec.NewDefault())
	if err != nil {
		return nil, fmt.Errorf("couldn't register codec: %w", err)
	}

	for _, chainID := range initiallyIndexedChains {
		if err := indexer.IndexChain(chainID); err != nil {
			return nil, fmt.Errorf("couldn't index chain %s: %w", chainID, err)
		}
	}
	return indexer, nil
}

// indexer implements Indexer
type indexer struct {
	name        string
	codec       codec.Manager
	lock        sync.RWMutex
	log         logging.Logger
	db          database.Database
	chainLookup func(string) (ids.ID, error)

	// Chain ID --> Index for that chain
	indexedChains map[ids.ID]Index

	// Indices are hooked up to the event dispatcher,
	// which notifies them of accepted containers
	eventDispatcher *triggers.EventDispatcher
}

func (i *indexer) IndexChain(chainID ids.ID) error {
	i.lock.Lock()
	defer i.lock.Unlock()

	indexDb := prefixdb.New(chainID[:], i.db)
	index, err := newIndex(indexDb, i.log, i.codec)
	if err != nil {
		return fmt.Errorf("couldn't create index for chain %s: %w", chainID, err)
	}
	err = i.eventDispatcher.RegisterChain(chainID, fmt.Sprintf("%s%s", indexNamePrefix, i.name), index)
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

// GetContainersByIndex ...
func (i *indexer) GetContainerByIndex(chainID ids.ID, indexToFetch uint64) (Container, error) {
	i.lock.RLock()
	defer i.lock.RUnlock()

	index, exists := i.indexedChains[chainID]
	if !exists {
		return Container{}, fmt.Errorf("there is no index for chain %s", chainID)
	}
	return index.GetContainerByIndex(indexToFetch)
}

// GetContainersByIndex ...
func (i *indexer) GetContainerRange(chainID ids.ID, startIndex, numToFetch uint64) ([]Container, error) {
	i.lock.RLock()
	defer i.lock.RUnlock()

	index, exists := i.indexedChains[chainID]
	if !exists {
		return nil, fmt.Errorf("there is no index for chain %s", chainID)
	}
	return index.GetContainerRange(startIndex, numToFetch)
}

func (i *indexer) GetLastAccepted(chainID ids.ID) (Container, error) {
	i.lock.RLock()
	defer i.lock.RUnlock()

	index, exists := i.indexedChains[chainID]
	if !exists {
		return Container{}, fmt.Errorf("there is no index for chain %s", chainID)
	}
	return index.GetLastAccepted()
}

func (i *indexer) GetIndex(chainID ids.ID, containerID ids.ID) (uint64, error) {
	i.lock.RLock()
	defer i.lock.RUnlock()

	index, exists := i.indexedChains[chainID]
	if !exists {
		return 0, fmt.Errorf("there is no index for chain %s", chainID)
	}
	return index.GetIndex(containerID)
}

// GetContainerByID ...
func (i *indexer) GetContainerByID(chainID ids.ID, containerID ids.ID) (Container, error) {
	i.lock.RLock()
	defer i.lock.RUnlock()

	index, exists := i.indexedChains[chainID]
	if !exists {
		return Container{}, fmt.Errorf("there is no index for chain %s", chainID)
	}
	return index.GetContainerByID(containerID)
}

func (i *indexer) Handler() (*common.HTTPHandler, error) {
	apiServer := rpc.NewServer()
	codec := cjson.NewCodec()
	apiServer.RegisterCodec(codec, "application/json")
	apiServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	if err := apiServer.RegisterService(&service{indexer: i}, "index"); err != nil {
		return nil, err
	}
	return &common.HTTPHandler{LockOptions: common.NoLock, Handler: apiServer}, nil
}

func (i *indexer) Close() error {
	i.lock.Lock()
	defer i.lock.Unlock()

	errs := &wrappers.Errs{}
	for chainID, index := range i.indexedChains {
		errs.Add(index.Close())
		delete(i.indexedChains, chainID)
	}
	errs.Add(i.db.Close())
	return errs.Err
}
