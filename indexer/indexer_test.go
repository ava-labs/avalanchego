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
	"github.com/ava-labs/avalanchego/snow/triggers"
	"github.com/ava-labs/avalanchego/utils"
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

// Test that the indexer works handles initially indexed chains properly
func TestIndexInitialChains(t *testing.T) {
	assert := assert.New(t)
	ed := &triggers.EventDispatcher{}
	ed.Initialize(logging.NoLog{})
	chain1ID, chain2ID := ids.GenerateTestID(), ids.GenerateTestID()
	initiallyIndexed := ids.Set{}
	initiallyIndexed.Add(chain1ID, chain2ID)
	chain1Ctx, chain2Ctx := snow.DefaultContextTest(), snow.DefaultContextTest()
	chain1Ctx.ChainID = chain1ID
	chain2Ctx.ChainID = chain2ID
	config := Config{
		IndexingEnabled:        true,
		AllowIncompleteIndex:   false,
		Log:                    logging.NoLog{},
		Name:                   "test",
		DB:                     memdb.New(),
		EventDispatcher:        ed,
		InitiallyIndexedChains: initiallyIndexed,
		ChainLookupF:           func(string) (ids.ID, error) { return ids.ID{}, nil },
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
	indexedChains := idxr.GetIndexedChains()
	assert.Len(indexedChains, 2)
	assert.True(
		indexedChains[0] == chain1ID && indexedChains[1] == chain2ID ||
			indexedChains[1] == chain1ID && indexedChains[0] == chain2ID)

	// Accept a transaction on chain1 and on chain2
	container1ID, container1Bytes := ids.GenerateTestID(), utils.RandomBytes(32)
	container2ID, container2Bytes := ids.GenerateTestID(), utils.RandomBytes(32)

	type test struct {
		chainCtx       *snow.Context
		containerID    ids.ID
		containerBytes []byte
	}

	tests := []test{
		{
			chain1Ctx,
			container1ID,
			container1Bytes,
		},
		{
			chain2Ctx,
			container2ID,
			container2Bytes,
		},
	}

	for _, test := range tests {
		now := time.Now()
		idxr.clock.Set(now)
		expectedContainer := Container{
			ID:        test.containerID,
			Bytes:     test.containerBytes,
			Timestamp: now.Unix(),
		}

		// Accept a container
		ed.Accept(test.chainCtx, test.containerID, test.containerBytes)

		// Verify GetLastAccepted is right
		gotLastAccepted, err := idxr.GetLastAccepted(test.chainCtx.ChainID)
		assert.NoError(err)
		assert.Equal(expectedContainer, gotLastAccepted)

		// Verify GetContainerByID is right
		container, err := idxr.GetContainerByID(test.chainCtx.ChainID, test.containerID)
		assert.NoError(err)
		assert.Equal(expectedContainer, container)

		// Verify GetIndex is right
		index, err := idxr.GetIndex(test.chainCtx.ChainID, test.containerID)
		assert.NoError(err)
		assert.EqualValues(0, index)

		// Verify GetContainerByIndex is right
		container, err = idxr.GetContainerByIndex(test.chainCtx.ChainID, 0)
		assert.NoError(err)
		assert.Equal(expectedContainer, container)

		// Verify GetContainerRange is right
		containers, err := idxr.GetContainerRange(test.chainCtx.ChainID, 0, 1)
		assert.NoError(err)
		assert.Len(containers, 1)
		assert.Equal(expectedContainer, containers[0])
	}
	assert.NoError(idxr.Close())
}

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
		EventDispatcher:        ed,
		InitiallyIndexedChains: ids.Set{}, // No chains indexed at start
		ChainLookupF:           func(string) (ids.ID, error) { return ids.ID{}, nil },
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
		EventDispatcher:        ed,
		InitiallyIndexedChains: initiallyIndexed,
		ChainLookupF:           func(string) (ids.ID, error) { return ids.ID{}, nil },
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
		EventDispatcher:        ed,
		InitiallyIndexedChains: initiallyIndexed,
		ChainLookupF:           func(string) (ids.ID, error) { return ids.ID{}, nil },
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
		EventDispatcher:        ed,
		InitiallyIndexedChains: nil, // no initially indexed chains
		ChainLookupF:           func(string) (ids.ID, error) { return ids.ID{}, nil },
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
