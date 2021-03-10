package indexer

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/utils/hashing"
	cjson "github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/timer"
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
	indexedChainsPrefix = []byte("indexed")
	hasEverRunPrefix    = []byte("hasEverRun")
	hasEverRunKey       = []byte("hasEverRun")
	incompleteIndexVal  = []byte{0}
	completeIndexVal    = []byte{1}
)

var (
	_ Indexer = &indexer{}
)

// Config for an indexer
type Config struct {
	Db  database.Database
	Log logging.Logger
	// If false, use a dummy (no-op) indexer
	IndexingEnabled bool
	// If true, allow indices that may be missing containers
	AllowIncompleteIndex bool
	// Name of this indexer and the API endpoint
	Name string
	// Notifies indexer of newly accepted containers
	EventDispatcher *triggers.EventDispatcher
	// Chains indexed on startup
	InitiallyIndexedChains ids.Set
	// Chain's Alias --> ID of that Chain
	ChainLookupF func(string) (ids.ID, error)
	// Used to register the indexer's API endpoint
	APIServer api.RouteAdder
}

// Indexer causes accepted containers for a given chain
// to be indexed by their ID and by the order in which
// they were accepted by this node.
// Indexer is threadsafe.
type Indexer interface {
	IndexChain(chainID ids.ID) error
	CloseIndex(chainID ids.ID) error
	GetContainerByIndex(chainID ids.ID, index uint64) (Container, error)
	GetContainerRange(chainID ids.ID, startIndex, numToFetch uint64) ([]Container, error)
	GetLastAccepted(chainID ids.ID) (Container, error)
	GetIndex(chainID, containerID ids.ID) (uint64, error)
	GetContainerByID(chainID, containerID ids.ID) (Container, error)
	GetIndexedChains() []ids.ID
	Close() error
}

// NewIndexer returns a new Indexer and registers a new endpoint on the given API server.
func NewIndexer(config Config) (Indexer, error) {
	// See if we have run with this database before
	hasEverRunDb := prefixdb.New(hasEverRunPrefix, config.Db)
	defer hasEverRunDb.Close()
	hasRunBefore, err := hasEverRunDb.Has(hasEverRunKey)
	if err != nil {
		return nil, fmt.Errorf("couldn't read from database: %w", err)
	}

	// Mark that we have now run with this database
	err = hasEverRunDb.Put(hasEverRunKey, nil)
	if err != nil {
		return nil, err
	}

	indexedChainsDb := prefixdb.New(indexedChainsPrefix, config.Db)
	needToCloseIndexedChainsDb := true
	defer func() {
		if needToCloseIndexedChainsDb {
			indexedChainsDb.Close()
		}
	}()

	// If a node runs while not indexing a chain then the index for that chain will be
	// incomplete (it will be missing containers.) By default, creation of incomplete
	// indices is disallowed. That is, if the node indexes a chain during one run,
	// then it must always index that chain so as to avoid being incomplete.
	// Creation of incomplete indices can be explicitly allowed in the config.
	if hasRunBefore {
		// Key: Chain ID. Present if this chain was ever indexed by this indexer.
		// Value:
		//   [incompleteIndexVal] if this index may be incomplete
		//   [completeIndexVal] if this index is complete
		iter := indexedChainsDb.NewIterator()
		defer iter.Release()

		// IDs of chains that now have incomplete indices
		var incompleteIndices []ids.ID
		for iter.Next() {
			// Parse chain ID from bytes
			chainIDBytes := iter.Key()
			if len(chainIDBytes) != hashing.HashLen {
				// Sanity check; this should never happen
				return nil, fmt.Errorf("unexpectedly got chain ID with %d bytes", len(chainIDBytes))
			}
			// We indexed chain [chainID] in a previous run
			var chainID ids.ID
			copy(chainID[:], chainIDBytes)

			if !config.InitiallyIndexedChains.Contains(chainID) || !config.IndexingEnabled {
				// This chain was previously indexed but now will not be.
				// This would make it incomplete.
				if !config.AllowIncompleteIndex {
					// Disallow node from running in order to prevent incomplete index.
					return nil, fmt.Errorf(
						"chain %s was previously indexed but now not requested to be indexed. "+
							"This would result in an incomplete index. Incomplete indices "+
							"can be explicitly allowed in the node config", chainID)
				}
				// Allow the node to run but mark indices as incomplete
				incompleteIndices = append(incompleteIndices, chainID)
			}
		}

		// Mark that these indices are incomplete
		for _, chainID := range incompleteIndices {
			err := indexedChainsDb.Put(chainID[:], incompleteIndexVal)
			if err != nil {
				return nil, err
			}
		}
	}

	if !config.IndexingEnabled {
		return NewNoOpIndexer(), nil
	}

	// Want this database to stay open since we continue to use it in [indexer]
	needToCloseIndexedChainsDb = false

	indexer := &indexer{
		name:                 config.Name,
		codec:                codec.NewDefaultManager(),
		log:                  config.Log,
		db:                   config.Db,
		allowIncompleteIndex: config.AllowIncompleteIndex,
		indexedChains:        indexedChainsDb,
		eventDispatcher:      config.EventDispatcher,
		chainToIndex:         make(map[ids.ID]Index),
		chainLookup:          config.ChainLookupF,
	}
	err = indexer.codec.RegisterCodec(codecVersion, linearcodec.NewDefault())
	if err != nil {
		return nil, fmt.Errorf("couldn't register codec: %w", err)
	}

	for _, chainID := range config.InitiallyIndexedChains.List() {
		if err := indexer.indexChain(chainID, hasRunBefore); err != nil {
			return nil, fmt.Errorf("couldn't index chain %s: %w", chainID, err)
		}
	}

	// Register API server
	indexAPIServer := rpc.NewServer()
	codec := cjson.NewCodec()
	indexAPIServer.RegisterCodec(codec, "application/json")
	indexAPIServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	if err := indexAPIServer.RegisterService(&service{indexer: indexer}, "index"); err != nil {
		return nil, err
	}
	handler := &common.HTTPHandler{LockOptions: common.NoLock, Handler: indexAPIServer}
	err = config.APIServer.AddRoute(handler, &sync.RWMutex{}, "index/", config.Name, config.Log)
	if err != nil {
		return nil, fmt.Errorf("couldnt add API route: %w", err)
	}
	return indexer, nil
}

// indexer implements Indexer
type indexer struct {
	name  string
	codec codec.Manager
	clock timer.Clock
	lock  sync.RWMutex
	log   logging.Logger
	db    database.Database
	// If true, allow running in such a way that could allow the creation
	// of an index which could be missing accepted containers.
	allowIncompleteIndex bool

	// A chain ID is present as a key in this database if it has ever been indexed
	// If this node has ever run while not indexing this chain, maps to false
	// Otherwise, maps to true
	indexedChains database.Database

	// Given a chain's alias, returns the chain's ID
	chainLookup func(string) (ids.ID, error)

	// Chain ID --> Index for that chain
	chainToIndex map[ids.ID]Index

	// Indices are hooked up to the event dispatcher,
	// which notifies them of accepted containers
	eventDispatcher *triggers.EventDispatcher
}

// IndexChain causes the indexer to start indexing the given chain.
// Returns an error if the chain is already being indexed.
func (i *indexer) IndexChain(chainID ids.ID) error {
	i.lock.Lock()
	defer i.lock.Unlock()

	return i.indexChain(chainID, true)
}

// [hasRunBefore] is true iff the node has run with this database before
func (i *indexer) indexChain(chainID ids.ID, hasRunBefore bool) error {
	if _, exists := i.chainToIndex[chainID]; exists {
		return fmt.Errorf("chain %s is already being indexed", chainID)
	}

	// See if this chain's index is complete
	indexComplete := true

	if hasRunBefore { // If this is a fresh database, index must be complete
		b, err := i.indexedChains.Get(chainID[:])
		if err != nil && err != database.ErrNotFound {
			return fmt.Errorf("couldn't read from database: %w", err)
		}

		// Mark that this index is incomplete, if applicable
		indexComplete = err == nil && bytes.Equal(b, completeIndexVal)
	}

	if indexComplete {
		err := i.indexedChains.Put(chainID[:], completeIndexVal)
		if err != nil {
			return err
		}
	} else { // Or incomplete
		err := i.indexedChains.Put(chainID[:], incompleteIndexVal)
		if err != nil {
			return err
		}
	}

	// If this index is incomplete and this is not allowed, error.
	if !indexComplete && !i.allowIncompleteIndex {
		return fmt.Errorf(
			"attempted to index previously non-indexed chain %s. "+
				"This would cause an incomplete index.  Incomplete indices "+
				"can be explicitly allowed in the node config", chainID)
	}

	// Database for the index to use
	indexDb := prefixdb.New(chainID[:], i.db)
	index, err := newIndex(indexDb, i.log, i.codec, i.clock)
	if err != nil {
		return fmt.Errorf("couldn't create index for chain %s: %w", chainID, err)
	}

	// Register index to learn about new accepted containers
	err = i.eventDispatcher.RegisterChain(chainID, fmt.Sprintf("%s%s", indexNamePrefix, i.name), index)
	if err != nil {
		_ = index.Close()
		return fmt.Errorf("couldn't register index for chain %s with event dispatcher: %w", chainID, err)
	}
	i.chainToIndex[chainID] = index

	return nil
}

// CloseIndex closes the index for the given chain. No more accepted containers
// will be indexed on this chain.
func (i *indexer) CloseIndex(chainID ids.ID) error {
	i.lock.Lock()
	defer i.lock.Unlock()

	return i.closeIndex(chainID)
}

// Assumes [i.lock] is held
func (i *indexer) closeIndex(chainID ids.ID) error {
	index, exists := i.chainToIndex[chainID]
	if !exists {
		return nil
	}
	delete(i.chainToIndex, chainID)
	errs := wrappers.Errs{}
	errs.Add(i.eventDispatcher.DeregisterChain(chainID, fmt.Sprintf("%s%s", indexNamePrefix, i.name)))
	errs.Add(index.Close())

	return errs.Err
}

// GetContainersByIndex returns a container from the given chain by its index.
func (i *indexer) GetContainerByIndex(chainID ids.ID, indexToFetch uint64) (Container, error) {
	i.lock.RLock()
	defer i.lock.RUnlock()

	index, exists := i.chainToIndex[chainID]
	if !exists {
		return Container{}, fmt.Errorf("there is no index for chain %s", chainID)
	}
	return index.GetContainerByIndex(indexToFetch)
}

// GetContainerRange returns the containers at indices [startIndex], [startIndex+1],...,[startIndex+numToFetch-1]
// on the given chain.
func (i *indexer) GetContainerRange(chainID ids.ID, startIndex, numToFetch uint64) ([]Container, error) {
	i.lock.RLock()
	defer i.lock.RUnlock()

	index, exists := i.chainToIndex[chainID]
	if !exists {
		return nil, fmt.Errorf("there is no index for chain %s", chainID)
	}
	return index.GetContainerRange(startIndex, numToFetch)
}

// GetLastAccepted returns the most recently accepted container
// on the given chain.
func (i *indexer) GetLastAccepted(chainID ids.ID) (Container, error) {
	i.lock.RLock()
	defer i.lock.RUnlock()

	index, exists := i.chainToIndex[chainID]
	if !exists {
		return Container{}, fmt.Errorf("there is no index for chain %s", chainID)
	}
	return index.GetLastAccepted()
}

// GetIndex returns the index of the given container on the given chain.
// The index is the number of containers that were accepted before the given one.
// For example, the second container accepted on a chain has index 1.
func (i *indexer) GetIndex(chainID ids.ID, containerID ids.ID) (uint64, error) {
	i.lock.RLock()
	defer i.lock.RUnlock()

	index, exists := i.chainToIndex[chainID]
	if !exists {
		return 0, fmt.Errorf("there is no index for chain %s", chainID)
	}
	return index.GetIndex(containerID)
}

// GetContainerByID returns the container with the given ID, which exists on the given chain.
func (i *indexer) GetContainerByID(chainID ids.ID, containerID ids.ID) (Container, error) {
	i.lock.RLock()
	defer i.lock.RUnlock()

	index, exists := i.chainToIndex[chainID]
	if !exists {
		return Container{}, fmt.Errorf("there is no index for chain %s", chainID)
	}
	return index.GetContainerByID(containerID)
}

// GetIndexedChains returns the list of chains that are currently being indexed
// and which can be queried.
func (i *indexer) GetIndexedChains() []ids.ID {
	i.lock.RLock()
	defer i.lock.RUnlock()

	if i.chainToIndex == nil || len(i.chainToIndex) == 0 {
		return nil
	}
	chainIDs := make([]ids.ID, len(i.chainToIndex))
	j := 0
	for chainID := range i.chainToIndex {
		chainIDs[j] = chainID
		j++
	}
	return chainIDs
}

// Close this indexer. Stops indexing all chains.
// Closes this indexer's database.
// Assumes Close is only called after the node is
// done making decisions.
func (i *indexer) Close() error {
	i.lock.Lock()
	defer i.lock.Unlock()

	errs := &wrappers.Errs{}
	for chainID := range i.chainToIndex {
		errs.Add(i.closeIndex(chainID))
	}
	errs.Add(i.indexedChains.Close())
	errs.Add(i.db.Close())
	return errs.Err
}
