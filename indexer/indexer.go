package indexer

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman"
	"github.com/ava-labs/avalanchego/snow/triggers"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/gorilla/rpc/v2"
)

const (
	indexNamePrefix = "index-"
	codecVersion    = uint16(0)
)

var (
	txPrefix                = []byte{0x01}
	vtxPrefix               = []byte{0x02}
	blockPrefix             = []byte{0x03}
	isIncompletePrefix      = []byte{0x04}
	previouslyIndexedPrefix = []byte{0x05}
	errClosed               = errors.New("indexer is closed")
)

var (
	_ Indexer = &indexer{}
)

// Config for an indexer
type Config struct {
	DB  database.Database
	Log logging.Logger
	// If false, use a dummy (no-op) indexer
	IndexingEnabled bool
	// If true, allow indices that may be missing containers
	AllowIncompleteIndex bool
	// Name of this indexer and the API endpoint
	Name string
	// TODO comment
	DecisionDispatcher, ConsensusDispatcher *triggers.EventDispatcher
	APIServer                               api.RouteAdder
	// Called once in a goroutine when closing
	ShutdownF func()
}

// Indexer causes accepted containers for a given chain
// to be indexed by their ID and by the order in which
// they were accepted by this node.
// Indexer is threadsafe.
type Indexer interface {
	RegisterChain(name string, chainID ids.ID, engine interface{})
	Close() error
}

// NewIndexer returns a new Indexer and registers a new endpoint on the given API server.
func NewIndexer(config Config) (Indexer, error) {
	indexer := &indexer{
		name:                 config.Name,
		codec:                codec.NewDefaultManager(),
		log:                  config.Log,
		db:                   config.DB,
		allowIncompleteIndex: config.AllowIncompleteIndex,
		indexingEnabled:      config.IndexingEnabled,
		consensusDispatcher:  config.ConsensusDispatcher,
		decisionDispatcher:   config.DecisionDispatcher,
		txIndices:            map[ids.ID]Index{},
		vtxIndices:           map[ids.ID]Index{},
		blockIndices:         map[ids.ID]Index{},
		routeAdder:           config.APIServer,
		shutdownF:            config.ShutdownF,
	}
	if err := indexer.codec.RegisterCodec(codecVersion, linearcodec.NewDefault()); err != nil {
		return nil, fmt.Errorf("couldn't register codec: %s", err)
	}

	return indexer, nil
}

// indexer implements Indexer
type indexer struct {
	name      string
	codec     codec.Manager
	clock     timer.Clock
	lock      sync.RWMutex
	log       logging.Logger
	db        database.Database
	closed    bool
	shutdownF func()

	routeAdder api.RouteAdder

	// If true, allow running in such a way that could allow the creation
	// of an index which could be missing accepted containers.
	allowIncompleteIndex bool

	indexingEnabled bool

	blockIndices map[ids.ID]Index
	vtxIndices   map[ids.ID]Index
	txIndices    map[ids.ID]Index

	// TODO comment
	consensusDispatcher *triggers.EventDispatcher
	decisionDispatcher  *triggers.EventDispatcher
}

// TODO comment
func (i *indexer) RegisterChain(name string, chainID ids.ID, engineIntf interface{}) {
	i.lock.Lock()
	defer i.lock.Unlock()

	if i.closed {
		i.log.Debug("not registering chain %s because indexer is closed", name)
		return
	} else if i.blockIndices[chainID] != nil || i.txIndices[chainID] != nil || i.vtxIndices[chainID] != nil {
		i.log.Warn("chain %s is already being indexed", chainID)
		return
	}

	// If the index is incomplete, make sure that's OK. Otherwise, cause node to die.
	isIncomplete, err := i.isIncomplete(chainID)
	if err != nil {
		i.log.Error("couldn't get whether chain %s is incomplete: %s", name, err)
		if err := i.close(); err != nil {
			i.log.Error("error while closing indexer: %s", err)
		}
		return
	} else if isIncomplete && !i.allowIncompleteIndex {
		i.log.Fatal("index %s is incomplete but incomplete indices are disabled", name)
		if err := i.close(); err != nil {
			i.log.Error("error while closing indexer: %s", err)
		}
		return
	}

	// See if this chain was indexed in a previous run
	previouslyIndexed, err := i.wasPreviouslyIndexed(chainID)
	if err != nil {
		i.log.Error("couldn't get whether chain %s was previously indexed: %s", name, err)
		if err := i.close(); err != nil {
			i.log.Error("error while closing indexer: %s", err)
		}
		return
	}

	if !i.indexingEnabled { // Indexing is disabled
		if previouslyIndexed && !i.allowIncompleteIndex {
			// We indexed this chain in a previous run but not in this run.
			// This would create an incomplete index, which is not allowed, so exit.
			i.log.Error("running would cause index %s would become incomplete but incomplete indices are disabled", name)
			if err := i.close(); err != nil {
				i.log.Error("error while closing indexer: %s", err)
			}
			return
		}

		// Creating an incomplete index is allowed. Mark index as incomplete.
		err := i.markIncomplete(chainID)
		if err == nil {
			return
		}
		i.log.Error("couldn't mark chain %s as incomplete: %s", name, err)
		if err := i.close(); err != nil {
			i.log.Error("error while closing indexer: %s", err)
		}
		return
	}

	// Mark that in this run, this chain was indexed
	if err := i.markPreviouslyIndexed(chainID); err != nil {
		i.log.Error("couldn't mark chain %s as indexed: %s", name, err)
		if err := i.close(); err != nil {
			i.log.Error("error while closing indexer: %s", err)
		}
		return
	}

	switch engineIntf.(type) {
	case snowman.Engine:
		prefix := make([]byte, hashing.HashLen+wrappers.ByteLen)
		copy(prefix[:], chainID[:])
		copy(prefix[hashing.HashLen:], blockPrefix)
		indexDB := prefixdb.New(prefix, i.db)
		index, err := newIndex(indexDB, i.log, i.codec, i.clock)
		if err != nil {
			_ = indexDB.Close()
			i.log.Error("couldn't create index for chain %s: %s", name, err)
			if err := i.close(); err != nil {
				i.log.Error("error while closing indexer: %s", err)
			}
			return
		}

		// Register index to learn about new accepted blocks
		if err := i.consensusDispatcher.RegisterChain(chainID, fmt.Sprintf("%s%s", indexNamePrefix, i.name), index); err != nil {
			_ = index.Close()
			_ = indexDB.Close()
			i.log.Error("couldn't register chain %s to dispatcher: %s", name, err)
			if err := i.close(); err != nil {
				i.log.Error("error while closing indexer: %s", err)
			}
			return
		}

		// Create an API endpoint for this index
		indexAPIServer := rpc.NewServer()
		codec := json.NewCodec()
		indexAPIServer.RegisterCodec(codec, "application/json")
		indexAPIServer.RegisterCodec(codec, "application/json;charset=UTF-8")
		if err := indexAPIServer.RegisterService(&service{Index: index}, "index"); err != nil {
			_ = index.Close()
			_ = indexDB.Close()
			i.log.Error("couldn't create index API server for chain %s: %s", name, err)
			if err := i.close(); err != nil {
				i.log.Error("error while closing indexer: %s", err)
			}
			return
		}
		handler := &common.HTTPHandler{LockOptions: common.NoLock, Handler: indexAPIServer}
		if err := i.routeAdder.AddRoute(handler, &sync.RWMutex{}, "index/"+name, "/block", i.log); err != nil {
			_ = index.Close()
			_ = indexDB.Close()
			i.log.Error("couldn't create index API server for chain %s: %s", name, err)
			if err := i.close(); err != nil {
				i.log.Error("error while closing indexer: %s", err)
			}
			return

		}

		i.blockIndices[chainID] = index
	case avalanche.Engine:
		{ // Create the tx index
			prefix := make([]byte, hashing.HashLen+wrappers.ByteLen)
			copy(prefix[:], chainID[:])
			copy(prefix[hashing.HashLen:], txPrefix)
			txIndexDB := prefixdb.New(prefix, i.db)
			txIndex, err := newIndex(txIndexDB, i.log, i.codec, i.clock)
			if err != nil {
				_ = txIndexDB.Close()
				i.log.Error("couldn't create tx index for chain %s: %s", name, err)
				if err := i.close(); err != nil {
					i.log.Error("error while closing indexer: %s", err)
				}
				return
			}

			// Register index to learn about new accepted txs
			if err := i.decisionDispatcher.RegisterChain(chainID, fmt.Sprintf("%s%s", indexNamePrefix, i.name), txIndex); err != nil {
				_ = txIndex.Close()
				_ = txIndexDB.Close()
				i.log.Error("couldn't register chain %s to decision dispatcher: %s", name, err)
				if err := i.close(); err != nil {
					i.log.Error("error while closing indexer: %s", err)
				}
				return
			}

			// Create an API endpoint for this index
			indexAPIServer := rpc.NewServer()
			codec := json.NewCodec()
			indexAPIServer.RegisterCodec(codec, "application/json")
			indexAPIServer.RegisterCodec(codec, "application/json;charset=UTF-8")
			if err := indexAPIServer.RegisterService(&service{Index: txIndex}, "index"); err != nil {
				_ = txIndex.Close()
				_ = txIndexDB.Close()
				i.log.Error("couldn't create index API server for chain %s: %s", name, err)
				if err := i.close(); err != nil {
					i.log.Error("error while closing indexer: %s", err)
				}
				return
			}
			handler := &common.HTTPHandler{LockOptions: common.NoLock, Handler: indexAPIServer}
			if err := i.routeAdder.AddRoute(handler, &sync.RWMutex{}, "index/"+name, "/tx", i.log); err != nil {
				_ = txIndex.Close()
				i.log.Error("couldn't create index API server for chain %s: %s", name, err)
				if err := i.close(); err != nil {
					i.log.Error("error while closing indexer: %s", err)
				}
				return
			}

			i.txIndices[chainID] = txIndex
		}
		{ // Create the vtx index
			prefix := make([]byte, hashing.HashLen+wrappers.ByteLen)
			copy(prefix[:], chainID[:])
			copy(prefix[hashing.HashLen:], vtxPrefix)
			vtxIndexDB := prefixdb.New(prefix, i.db)
			vtxIndex, err := newIndex(vtxIndexDB, i.log, i.codec, i.clock)
			if err != nil {
				_ = vtxIndexDB.Close()
				i.log.Error("couldn't create vtx index for chain %s: %s", name, err)
				if err := i.close(); err != nil {
					i.log.Error("error while closing indexer: %s", err)
				}
				return
			}

			// Register index to learn about new accepted vertices
			if err := i.consensusDispatcher.RegisterChain(chainID, fmt.Sprintf("%s%s", indexNamePrefix, i.name), vtxIndex); err != nil {
				_ = vtxIndex.Close()
				_ = vtxIndexDB.Close()
				i.log.Error("couldn't register chain %s to decision dispatcher: %s", name, err)
				if err := i.close(); err != nil {
					i.log.Error("error while closing indexer: %s", err)
				}
				return
			}

			// Create an API endpoint for this index
			indexAPIServer := rpc.NewServer()
			codec := json.NewCodec()
			indexAPIServer.RegisterCodec(codec, "application/json")
			indexAPIServer.RegisterCodec(codec, "application/json;charset=UTF-8")
			if err := indexAPIServer.RegisterService(&service{Index: vtxIndex}, "index"); err != nil {
				_ = vtxIndex.Close()
				_ = vtxIndexDB.Close()
				i.log.Error("couldn't create index API server for chain %s: %s", name, err)
				if err := i.close(); err != nil {
					i.log.Error("error while closing indexer: %s", err)
				}
				return
			}
			handler := &common.HTTPHandler{LockOptions: common.NoLock, Handler: indexAPIServer}
			if err := i.routeAdder.AddRoute(handler, &sync.RWMutex{}, "index/"+name, "/vtx", i.log); err != nil {
				_ = vtxIndex.Close()
				i.log.Error("couldn't create index API server for chain %s: %s", name, err)
				if err := i.close(); err != nil {
					i.log.Error("error while closing indexer: %s", err)
				}
				return
			}

			i.vtxIndices[chainID] = vtxIndex
		}
	default:
		i.log.Error("expected snowman.Engine or avalanche.Engine but got %T", engineIntf)
	}
}

// Close this indexer. Stops indexing all chains.
// Closes [i.db]. Assumes Close is only called after
// the node is done making decisions.
// Calling Close after it has been called does nothing.
func (i *indexer) Close() error {
	i.lock.Lock()
	defer i.lock.Unlock()

	return i.close()
}

func (i *indexer) close() error {
	if i.closed {
		return nil
	}
	i.closed = true

	errs := &wrappers.Errs{}
	for chainID, txIndex := range i.txIndices {
		errs.Add(txIndex.Close())
		errs.Add(i.decisionDispatcher.DeregisterChain(chainID, fmt.Sprintf("%s%s", indexNamePrefix, i.name)))
	}
	for chainID, vtxIndex := range i.vtxIndices {
		errs.Add(vtxIndex.Close())
		errs.Add(i.consensusDispatcher.DeregisterChain(chainID, fmt.Sprintf("%s%s", indexNamePrefix, i.name)))
	}
	for chainID, blockIndex := range i.blockIndices {
		errs.Add(blockIndex.Close())
		errs.Add(i.consensusDispatcher.DeregisterChain(chainID, fmt.Sprintf("%s%s", indexNamePrefix, i.name)))
	}
	errs.Add(i.db.Close())

	go i.shutdownF()
	return errs.Err
}

func (i *indexer) markIncomplete(chainID ids.ID) error {
	key := make([]byte, hashing.HashLen+wrappers.ByteLen)
	copy(key[:], chainID[:])
	copy(key[hashing.HashLen:], isIncompletePrefix)
	return i.db.Put(key, nil)
}

// Returns true if this chain is incomplete
func (i *indexer) isIncomplete(chainID ids.ID) (bool, error) {
	key := make([]byte, hashing.HashLen+wrappers.ByteLen)
	copy(key[:], chainID[:])
	copy(key[hashing.HashLen:], isIncompletePrefix)
	return i.db.Has(key)
}

func (i *indexer) markPreviouslyIndexed(chainID ids.ID) error {
	key := make([]byte, hashing.HashLen+wrappers.ByteLen)
	copy(key[:], chainID[:])
	copy(key[hashing.HashLen:], previouslyIndexedPrefix)
	return i.db.Put(key, nil)
}

// Returns true if this chain is incomplete
func (i *indexer) wasPreviouslyIndexed(chainID ids.ID) (bool, error) {
	key := make([]byte, hashing.HashLen+wrappers.ByteLen)
	copy(key[:], chainID[:])
	copy(key[hashing.HashLen:], previouslyIndexedPrefix)
	return i.db.Has(key)
}
