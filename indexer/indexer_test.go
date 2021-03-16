package indexer

import (
	"io"
	"sync"
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/triggers"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/stretchr/testify/assert"
)

type apiServerMock struct {
	timesCalled int
}

func (a *apiServerMock) AddRoute(_ *common.HTTPHandler, _ *sync.RWMutex, _, _ string, _ io.Writer) error {
	a.timesCalled++
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
	assert.NotNil(idxr.txIndices)
	assert.NotNil(idxr.vtxIndices)
	assert.NotNil(idxr.consensusDispatcher)
	assert.NotNil(idxr.decisionDispatcher)
	assert.NotNil(idxr.shutdownF)
	assert.False(idxr.hasRunBefore)
}

// Test that [hasRunBefore] is set correctly
func TestMarkHasRun(t *testing.T) {
	assert := assert.New(t)
	ed := &triggers.EventDispatcher{}
	ed.Initialize(logging.NoLog{})
	baseDB := memdb.New()
	db := versiondb.New(baseDB)
	config := Config{
		IndexingEnabled:      true,
		AllowIncompleteIndex: true,
		Log:                  logging.NoLog{},
		Name:                 "test",
		DB:                   db,
		ConsensusDispatcher:  ed,
		DecisionDispatcher:   ed,
		APIServer:            &apiServerMock{},
		ShutdownF:            func() {},
	}

	idxrIntf, err := NewIndexer(config)
	assert.NoError(err)
	assert.NoError(db.Commit())
	assert.NoError(idxrIntf.Close())

	config.DB = versiondb.New(baseDB)
	idxrIntf, err = NewIndexer(config)
	assert.NoError(err)
	idxr, ok := idxrIntf.(*indexer)
	assert.True(ok)
	assert.True(idxr.hasRunBefore)
}

/*

// Test that chains added to indexer with IndexChain work
func TestIndexChain(t *testing.T) {
	assert := assert.New(t)
	ed := &triggers.EventDispatcher{}
	ed.Initialize(logging.NoLog{})
	config := Config{
		IndexingEnabled:        true,
		AllowIncompleteIndex:   false,
		Log:                    logging.NoLog{},
		Name:                   "test",
		DB:                     memdb.New(),
		ConsensusDispatcher:        ed,
		DecisionsDispatcher: ed,
		InitiallyIndexedChains: ids.Set{}, // No chains indexed at start
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
	assert.Len(idxr.GetIndexedChains(), 0)

	ctx := snow.DefaultContextTest()
	ctx.ChainID = ids.GenerateTestID()
	err = idxr.IndexChain(ctx.ChainID)
	assert.Error(err)                // Should error because incomplete indices not allowed
	idxr.allowIncompleteIndex = true // Allow incomplete index
	err = idxr.IndexChain(ctx.ChainID)
	assert.NoError(err) // Should succeed now

	containerID, containerBytes := ids.GenerateTestID(), utils.RandomBytes(32)
	now := time.Now()
	idxr.clock.Set(now)
	expectedContainer := Container{
		ID:        containerID,
		Bytes:     containerBytes,
		Timestamp: now.Unix(),
	}

	// Accept a container
	ed.Accept(ctx, containerID, containerBytes)

	// Verify GetLastAccepted is right
	gotLastAccepted, err := idxr.GetLastAccepted(ctx.ChainID)
	assert.NoError(err)
	assert.Equal(expectedContainer, gotLastAccepted)

	// Verify GetContainerByID is right
	container, err := idxr.GetContainerByID(ctx.ChainID, containerID)
	assert.NoError(err)
	assert.Equal(expectedContainer, container)

	// Verify GetIndex is right
	index, err := idxr.GetIndex(ctx.ChainID, containerID)
	assert.NoError(err)
	assert.EqualValues(0, index)

	// Verify GetContainerByIndex is right
	container, err = idxr.GetContainerByIndex(ctx.ChainID, 0)
	assert.NoError(err)
	assert.Equal(expectedContainer, container)

	// Verify GetContainerRange is right
	containers, err := idxr.GetContainerRange(ctx.ChainID, 0, 1)
	assert.NoError(err)
	assert.Len(containers, 1)
	assert.Equal(expectedContainer, containers[0])

	assert.NoError(idxr.Close())
}

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
		DecisionsDispatcher: ed,
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
		DecisionsDispatcher: ed,
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
		DecisionsDispatcher: ed,
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
