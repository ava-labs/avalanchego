package indexer

import (
	"io"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/triggers"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/stretchr/testify/assert"
)

type apiServerMock struct {
	timesCalled int
	bases       []string
	endpoints   []string
}

func (a *apiServerMock) AddRoute(_ *common.HTTPHandler, _ *sync.RWMutex, base, endpoint string, _ io.Writer) error {
	a.timesCalled++
	a.bases = append(a.bases, base)
	a.endpoints = append(a.endpoints, endpoint)
	return nil
}

// Test that newIndexer sets fields correctly
func TestNewIndexer(t *testing.T) {
	assert := assert.New(t)
	ed := &triggers.EventDispatcher{}
	ed.Initialize(logging.NoLog{})
	config := Config{
		IndexingEnabled:      true,
		AllowIncompleteIndex: true,
		Log:                  logging.NoLog{},
		Name:                 "test",
		DB:                   memdb.New(),
		ConsensusDispatcher:  ed,
		DecisionDispatcher:   ed,
		APIServer:            &apiServerMock{},
		ShutdownF:            func() {},
	}

	idxrIntf, err := NewIndexer(config)
	assert.NoError(err)
	idxr, ok := idxrIntf.(*indexer)
	assert.True(ok)
	assert.Equal("test", idxr.name)
	assert.NotNil(idxr.codec)
	assert.NotNil(idxr.log)
	assert.NotNil(idxr.db)
	assert.False(idxr.closed)
	assert.NotNil(idxr.routeAdder)
	assert.True(idxr.indexingEnabled)
	assert.True(idxr.allowIncompleteIndex)
	assert.NotNil(idxr.blockIndices)
	assert.Len(idxr.blockIndices, 0)
	assert.NotNil(idxr.txIndices)
	assert.Len(idxr.txIndices, 0)
	assert.NotNil(idxr.vtxIndices)
	assert.Len(idxr.vtxIndices, 0)
	assert.NotNil(idxr.consensusDispatcher)
	assert.NotNil(idxr.decisionDispatcher)
	assert.NotNil(idxr.shutdownF)
	assert.False(idxr.hasRunBefore)
}

// Test that [hasRunBefore] is set correctly
func TestMarkHasRun(t *testing.T) {
	assert := assert.New(t)
	cd := &triggers.EventDispatcher{}
	cd.Initialize(logging.NoLog{})
	dd := &triggers.EventDispatcher{}
	dd.Initialize(logging.NoLog{})
	baseDB := memdb.New()
	db := versiondb.New(baseDB)
	shutdown := &sync.WaitGroup{}
	shutdown.Add(1)
	config := Config{
		IndexingEnabled:     true,
		Log:                 logging.NoLog{},
		Name:                "test",
		DB:                  db,
		ConsensusDispatcher: cd,
		DecisionDispatcher:  dd,
		APIServer:           &apiServerMock{},
		ShutdownF:           func() { shutdown.Done() },
	}

	idxrIntf, err := NewIndexer(config)
	assert.NoError(err)
	assert.False(idxrIntf.(*indexer).hasRunBefore)
	assert.NoError(db.Commit())
	assert.NoError(idxrIntf.Close())
	shutdown.Wait()
	shutdown.Add(1)

	config.DB = versiondb.New(baseDB)
	idxrIntf, err = NewIndexer(config)
	assert.NoError(err)
	idxr, ok := idxrIntf.(*indexer)
	assert.True(ok)
	assert.True(idxr.hasRunBefore)
	assert.NoError(idxr.Close())
	shutdown.Wait()
}

// Test that snowman chains added to indexer with RegisterChain work
func TestRegisterSnowmanChain(t *testing.T) {
	assert := assert.New(t)
	cd := &triggers.EventDispatcher{}
	cd.Initialize(logging.NoLog{})
	dd := &triggers.EventDispatcher{}
	dd.Initialize(logging.NoLog{})
	baseDB := memdb.New()
	db := versiondb.New(baseDB)
	config := Config{
		IndexingEnabled:      true,
		AllowIncompleteIndex: false,
		Log:                  logging.NoLog{},
		Name:                 "test",
		DB:                   db,
		ConsensusDispatcher:  cd,
		DecisionDispatcher:   dd,
		APIServer:            &apiServerMock{},
		ShutdownF:            func() { t.Fatal("shouldn't have shut down") },
	}

	// Create indexer
	idxrIntf, err := NewIndexer(config)
	assert.NoError(err)
	idxr, ok := idxrIntf.(*indexer)
	assert.True(ok)

	// Assert state is right
	ctx := snow.DefaultContextTest()
	ctx.ChainID = ids.GenerateTestID()
	isIncomplete, err := idxr.isIncomplete(ctx.ChainID)
	assert.NoError(err)
	assert.False(isIncomplete)
	previouslyIndexed, err := idxr.previouslyIndexed(ctx.ChainID)
	assert.NoError(err)
	assert.False(previouslyIndexed)

	// Register this chain, creating a new index
	vm := &block.TestVM{}
	idxr.RegisterChain("chain1", ctx, vm)
	isIncomplete, err = idxr.isIncomplete(ctx.ChainID)
	assert.NoError(err)
	assert.False(isIncomplete)
	previouslyIndexed, err = idxr.previouslyIndexed(ctx.ChainID)
	assert.NoError(err)
	assert.True(previouslyIndexed)
	server := config.APIServer.(*apiServerMock)
	assert.EqualValues(1, server.timesCalled)
	assert.EqualValues("index/chain1", server.bases[0])
	assert.EqualValues("/block", server.endpoints[0])
	assert.Len(idxr.blockIndices, 1)
	assert.Len(idxr.txIndices, 0)
	assert.Len(idxr.vtxIndices, 0)

	// Accept a container
	containerID, containerBytes := ids.GenerateTestID(), utils.RandomBytes(32)
	now := time.Now()
	idxr.clock.Set(now)
	expectedContainer := Container{
		ID:        containerID,
		Bytes:     containerBytes,
		Timestamp: now.Unix(),
	}

	cd.Accept(ctx, containerID, containerBytes)

	idx := idxr.blockIndices[ctx.ChainID]
	assert.NotNil(idx)

	// Verify GetLastAccepted is right
	gotLastAccepted, err := idx.GetLastAccepted()
	assert.NoError(err)
	assert.Equal(expectedContainer, gotLastAccepted)

	// Verify GetContainerByID is right
	container, err := idx.GetContainerByID(containerID)
	assert.NoError(err)
	assert.Equal(expectedContainer, container)

	// Verify GetIndex is right
	index, err := idx.GetIndex(containerID)
	assert.NoError(err)
	assert.EqualValues(0, index)

	// Verify GetContainerByIndex is right
	container, err = idx.GetContainerByIndex(0)
	assert.NoError(err)
	assert.Equal(expectedContainer, container)

	// Verify GetContainerRange is right
	containers, err := idx.GetContainerRange(0, 1)
	assert.NoError(err)
	assert.Len(containers, 1)
	assert.Equal(expectedContainer, containers[0])

	// Close the indexer
	assert.NoError(db.Commit())
	idxr.shutdownF = func() {} // Don't error on shutdown
	assert.NoError(idxr.Close())
	assert.True(idxr.closed)
	// Calling Close again should be fine
	assert.NoError(idxr.Close())
}

/*
// Test method CloseIndex
func TestCloseIndex(t *testing.T) {
	// Setup
	assert := assert.New(t)
	ed := &triggers.EventDispatcher{}
	ed.Initialize(logging.NoLog{})
	chain1ID := ids.GenerateTestID()
	initiallyIndexed := ids.Set{}
	initiallyIndexed.Add(chain1ID)
	chain1Ctx := snow.DefaultContextTest()
	chain1Ctx.ChainID = chain1ID
	config := Config{
		IndexingEnabled:        true,
		AllowIncompleteIndex:   false,
		Log:                    logging.NoLog{},
		Name:                   "test",
		DB:                     memdb.New(),
		ConsensusDispatcher:        ed,
		DecisionDispatcher: ed,
		APIServer:              &apiServerMock{},
	}

	// Create indexer and make sure its state is right
	idxrIntf, err := NewIndexer(config)
	assert.NoError(err)
	idxr, ok := idxrIntf.(*indexer)
	assert.True(ok)
	assert.Equal(config.Name, idxr.name)
	assert.NotNil(idxr.log)
	assert.NotNil(idxr.db)
	assert.NotNil(idxr.indexedChains)
	assert.False(idxr.allowIncompleteIndex)
	assert.NotNil(idxr.chainLookup)
	assert.NotNil(idxr.codec)
	assert.NotNil(idxr.chainToIndex)
	assert.Equal(1, config.APIServer.(*apiServerMock).timesCalled)
	assert.Len(idxr.GetIndexedChains(), 1)

	// Stop indexing a non-existent chain (shouldn't do anything)
	err = idxr.CloseIndex(ids.GenerateTestID())
	assert.NoError(err)
	assert.Len(idxr.GetIndexedChains(), 1)

	// Stop indexing a chain
	err = idxr.CloseIndex(chain1ID)
	assert.NoError(err)
	assert.Len(idxr.GetIndexedChains(), 0)

	// Shouldn't be able to get things from this index anymore
	_, err = idxr.GetLastAccepted(chain1ID)
	assert.Error(err)
}

// Test that indexer doesn't allow an incomplete index
// unless that is allowed in the config
func TestIncompleteIndexStartup(t *testing.T) {
	// Setup
	assert := assert.New(t)
	ed := &triggers.EventDispatcher{}
	ed.Initialize(logging.NoLog{})
	chain1ID := ids.GenerateTestID()
	initiallyIndexed := ids.Set{}
	initiallyIndexed.Add(chain1ID)
	chain1Ctx := snow.DefaultContextTest()
	chain1Ctx.ChainID = chain1ID
	db := memdb.New()
	defer db.Close()
	dbCopy := versiondb.New(db) // Will be written to [db]
	config := Config{
		IndexingEnabled:        true,
		AllowIncompleteIndex:   false,
		Log:                    logging.NoLog{},
		Name:                   "test",
		DB:                     dbCopy,
		ConsensusDispatcher:        ed,
		DecisionDispatcher: ed,
		APIServer:              &apiServerMock{},
	}

	// Create indexer with incomplete index disallowed
	idxrIntf, err := NewIndexer(config)
	assert.NoError(err)
	idxr, ok := idxrIntf.(*indexer)
	assert.True(ok)

	// Close the indexer after copying its contents to [db]
	assert.NoError(dbCopy.Commit())
	assert.NoError(idxr.Close())

	// Re-open the indexer. Should be allowed since we never ran without indexing chain1.
	dbCopy = versiondb.New(db) // Because [dbCopy] was closed when indexer closed
	config.DB = dbCopy
	idxrIntf, err = NewIndexer(config)
	assert.NoError(err)
	idxr, ok = idxrIntf.(*indexer)
	assert.True(ok)
	// Close the indexer again
	assert.NoError(dbCopy.Commit())
	assert.NoError(idxr.Close())

	// Re-open the indexer with indexing disabled.
	dbCopy = versiondb.New(db) // Because [dbCopy] was closed when indexer closed
	config.DB = dbCopy
	config.IndexingEnabled = false
	_, err = NewIndexer(config)
	assert.NoError(dbCopy.Commit())
	assert.Error(err) // Should error because running would cause incomplete index

	config.AllowIncompleteIndex = true // allow incomplete index
	idxrIntf, err = NewIndexer(config)
	assert.NoError(err)
	assert.NoError(dbCopy.Commit())
	assert.NoError(idxrIntf.Close()) // close the indexer

	dbCopy = versiondb.New(db) // Because [dbCopy] was closed when indexer closed
	config.DB = dbCopy
	config.AllowIncompleteIndex = false
	config.IndexingEnabled = true
	_, err = NewIndexer(config)
	assert.NoError(dbCopy.Commit())
	assert.Error(err) // Should error because we have an incomplete index
}

// Test that indexer doesn't allow an incomplete index
// unless that is allowed in the config
func TestIncompleteIndexNewChain(t *testing.T) {
	// Setup
	assert := assert.New(t)
	ed := &triggers.EventDispatcher{}
	ed.Initialize(logging.NoLog{})

	db := memdb.New()
	defer db.Close()
	dbCopy := versiondb.New(db) // Will be written to [db]
	config := Config{
		IndexingEnabled:        true,
		AllowIncompleteIndex:   false,
		Log:                    logging.NoLog{},
		Name:                   "test",
		DB:                     dbCopy,
		ConsensusDispatcher:        ed,
		DecisionDispatcher: ed,
		InitiallyIndexedChains: nil, // no initially indexed chains
		APIServer:              &apiServerMock{},
	}

	// Create indexer with incomplete index disallowed
	idxrIntf, err := NewIndexer(config)
	assert.NoError(err)
	idxr, ok := idxrIntf.(*indexer)
	assert.True(ok)

	// Should error because indexing new chain would cause incomplete index
	err = idxr.IndexChain(ids.GenerateTestID())
	assert.Error(err)
	assert.NoError(idxr.Close())

	// Allow incomplete index
	dbCopy = versiondb.New(db)
	config.DB = dbCopy
	config.AllowIncompleteIndex = true
	idxrIntf, err = NewIndexer(config)
	assert.NoError(err)
	idxr, ok = idxrIntf.(*indexer)
	assert.True(ok)

	err = idxr.IndexChain(ids.GenerateTestID()) // Should allow incomplete index
	assert.NoError(err)
	assert.NoError(idxr.Close())
}
*/
