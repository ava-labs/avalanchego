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
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/mocks"
	avvtxmocks "github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex/mocks"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	smblockmocks "github.com/ava-labs/avalanchego/snow/engine/snowman/block/mocks"
	smengmocks "github.com/ava-labs/avalanchego/snow/engine/snowman/mocks"
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

// Test registering a linear chain and a DAG chain and accepting
// some vertices
func TestIndexer(t *testing.T) {
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
		ShutdownF:            func() {},
	}

	// Create indexer
	idxrIntf, err := NewIndexer(config)
	assert.NoError(err)
	idxr, ok := idxrIntf.(*indexer)
	assert.True(ok)

	// Assert state is right
	chain1Ctx := snow.DefaultContextTest()
	chain1Ctx.ChainID = ids.GenerateTestID()
	isIncomplete, err := idxr.isIncomplete(chain1Ctx.ChainID)
	assert.NoError(err)
	assert.False(isIncomplete)
	previouslyIndexed, err := idxr.previouslyIndexed(chain1Ctx.ChainID)
	assert.NoError(err)
	assert.False(previouslyIndexed)

	// Register this chain, creating a new index
	chainVM := &smblockmocks.ChainVM{}
	chainEngine := &smengmocks.SnowmanEngine{}
	chainEngine.On("GetVM").Return(chainVM)

	idxr.RegisterChain("chain1", chain1Ctx, chainEngine)
	isIncomplete, err = idxr.isIncomplete(chain1Ctx.ChainID)
	assert.NoError(err)
	assert.False(isIncomplete)
	previouslyIndexed, err = idxr.previouslyIndexed(chain1Ctx.ChainID)
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
	blkID, blkBytes := ids.GenerateTestID(), utils.RandomBytes(32)
	now := time.Now()
	idxr.clock.Set(now)
	expectedContainer := Container{
		ID:        blkID,
		Bytes:     blkBytes,
		Timestamp: now.Unix(),
	}
	// Mocked VM knows about this block now
	chainVM.On("GetBlock", blkID).Return(
		&snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				StatusV: choices.Accepted,
				IDV:     blkID,
			},
			BytesV: blkBytes,
		}, nil,
	).Twice()

	cd.Accept(chain1Ctx, blkID, blkBytes)

	blkIdx := idxr.blockIndices[chain1Ctx.ChainID]
	assert.NotNil(blkIdx)

	// Verify GetLastAccepted is right
	gotLastAccepted, err := blkIdx.GetLastAccepted()
	assert.NoError(err)
	assert.Equal(expectedContainer, gotLastAccepted)

	// Verify GetContainerByID is right
	container, err := blkIdx.GetContainerByID(blkID)
	assert.NoError(err)
	assert.Equal(expectedContainer, container)

	// Verify GetIndex is right
	index, err := blkIdx.GetIndex(blkID)
	assert.NoError(err)
	assert.EqualValues(0, index)

	// Verify GetContainerByIndex is right
	container, err = blkIdx.GetContainerByIndex(0)
	assert.NoError(err)
	assert.Equal(expectedContainer, container)

	// Verify GetContainerRange is right
	containers, err := blkIdx.GetContainerRange(0, 1)
	assert.NoError(err)
	assert.Len(containers, 1)
	assert.Equal(expectedContainer, containers[0])

	// Close the indexer
	assert.NoError(db.Commit())
	assert.NoError(idxr.Close())
	assert.True(idxr.closed)
	// Calling Close again should be fine
	assert.NoError(idxr.Close())
	server.timesCalled = 0

	// Re-open the indexer
	config.DB = versiondb.New(baseDB)
	idxrIntf, err = NewIndexer(config)
	assert.NoError(err)
	idxr, ok = idxrIntf.(*indexer)
	assert.True(ok)
	assert.Len(idxr.blockIndices, 0)
	assert.Len(idxr.txIndices, 0)
	assert.Len(idxr.vtxIndices, 0)
	assert.True(idxr.hasRunBefore)
	previouslyIndexed, err = idxr.previouslyIndexed(chain1Ctx.ChainID)
	assert.NoError(err)
	assert.True(previouslyIndexed)
	hasRun, err := idxr.hasRun()
	assert.NoError(err)
	assert.True(hasRun)
	isIncomplete, err = idxr.isIncomplete(chain1Ctx.ChainID)
	assert.NoError(err)
	assert.False(isIncomplete)

	// Register the same chain as before
	idxr.RegisterChain("chain1", chain1Ctx, chainEngine)
	blkIdx = idxr.blockIndices[chain1Ctx.ChainID]
	assert.NotNil(blkIdx)
	container, err = blkIdx.GetLastAccepted()
	assert.NoError(err)
	assert.Equal(blkID, container.ID)

	// Register a DAG chain
	chain2Ctx := snow.DefaultContextTest()
	chain2Ctx.ChainID = ids.GenerateTestID()
	isIncomplete, err = idxr.isIncomplete(chain2Ctx.ChainID)
	assert.NoError(err)
	assert.False(isIncomplete)
	previouslyIndexed, err = idxr.previouslyIndexed(chain2Ctx.ChainID)
	assert.NoError(err)
	assert.False(previouslyIndexed)
	dagVM := &avvtxmocks.DAGVM{}
	dagEngine := &mocks.Engine{}
	dagEngine.On("GetVM").Return(dagVM).Once()
	idxr.RegisterChain("chain2", chain2Ctx, dagEngine)
	assert.NoError(err)
	server = config.APIServer.(*apiServerMock)
	assert.EqualValues(3, server.timesCalled) // block index, vtx index, tx index
	assert.Contains(server.bases, "index/chain2")
	assert.Contains(server.endpoints, "/vtx")
	assert.Contains(server.endpoints, "/tx")
	assert.Len(idxr.blockIndices, 1)
	assert.Len(idxr.txIndices, 1)
	assert.Len(idxr.vtxIndices, 1)

	// Accept a vertex
	vtxID, vtxBytes := ids.GenerateTestID(), utils.RandomBytes(32)
	now = time.Now()
	idxr.clock.Set(now)
	expectedVtx := Container{
		ID:        vtxID,
		Bytes:     blkBytes,
		Timestamp: now.Unix(),
	}
	// Mocked VM knows about this block now
	dagEngine.On("GetVtx", vtxID).Return(
		&avalanche.TestVertex{
			TestDecidable: choices.TestDecidable{
				StatusV: choices.Accepted,
				IDV:     vtxID,
			},
			BytesV: vtxBytes,
		}, nil,
	).Once()

	cd.Accept(chain2Ctx, vtxID, blkBytes)

	vtxIdx := idxr.vtxIndices[chain2Ctx.ChainID]
	assert.NotNil(vtxIdx)

	// Verify GetLastAccepted is right
	gotLastAccepted, err = vtxIdx.GetLastAccepted()
	assert.NoError(err)
	assert.Equal(expectedVtx, gotLastAccepted)

	// Verify GetContainerByID is right
	vtx, err := vtxIdx.GetContainerByID(vtxID)
	assert.NoError(err)
	assert.Equal(expectedVtx, vtx)

	// Verify GetIndex is right
	index, err = vtxIdx.GetIndex(vtxID)
	assert.NoError(err)
	assert.EqualValues(0, index)

	// Verify GetContainerByIndex is right
	vtx, err = vtxIdx.GetContainerByIndex(0)
	assert.NoError(err)
	assert.Equal(expectedVtx, vtx)

	// Verify GetContainerRange is right
	vtxs, err := vtxIdx.GetContainerRange(0, 1)
	assert.NoError(err)
	assert.Len(vtxs, 1)
	assert.Equal(expectedVtx, vtxs[0])

	// Accept a tx
	txID, txBytes := ids.GenerateTestID(), utils.RandomBytes(32)
	now = time.Now()
	idxr.clock.Set(now)
	expectedTx := Container{
		ID:        txID,
		Bytes:     blkBytes,
		Timestamp: now.Unix(),
	}
	// Mocked VM knows about this tx now
	dagVM.On("GetTx", txID).Return(
		&snowstorm.TestTx{
			TestDecidable: choices.TestDecidable{
				IDV:     txID,
				StatusV: choices.Accepted,
			},
			BytesV: txBytes,
		}, nil,
	).Once()

	dd.Accept(chain2Ctx, txID, blkBytes)

	txIdx := idxr.txIndices[chain2Ctx.ChainID]
	assert.NotNil(txIdx)

	// Verify GetLastAccepted is right
	gotLastAccepted, err = txIdx.GetLastAccepted()
	assert.NoError(err)
	assert.Equal(expectedTx, gotLastAccepted)

	// Verify GetContainerByID is right
	tx, err := txIdx.GetContainerByID(txID)
	assert.NoError(err)
	assert.Equal(expectedTx, tx)

	// Verify GetIndex is right
	index, err = txIdx.GetIndex(txID)
	assert.NoError(err)
	assert.EqualValues(0, index)

	// Verify GetContainerByIndex is right
	tx, err = txIdx.GetContainerByIndex(0)
	assert.NoError(err)
	assert.Equal(expectedTx, tx)

	// Verify GetContainerRange is right
	txs, err := txIdx.GetContainerRange(0, 1)
	assert.NoError(err)
	assert.Len(txs, 1)
	assert.Equal(expectedTx, txs[0])

	// Accepting a vertex shouldn't have caused anything to
	// happen on the block/tx index. Similar for tx.
	lastAcceptedTx, err := txIdx.GetLastAccepted()
	assert.NoError(err)
	assert.EqualValues(txID, lastAcceptedTx.ID)
	lastAcceptedVtx, err := vtxIdx.GetLastAccepted()
	assert.NoError(err)
	assert.EqualValues(vtxID, lastAcceptedVtx.ID)
	lastAcceptedBlk, err := blkIdx.GetLastAccepted()
	assert.NoError(err)
	assert.EqualValues(blkID, lastAcceptedBlk.ID)

	// Close the indexer again
	assert.NoError(config.DB.(*versiondb.Database).Commit())
	assert.NoError(idxr.Close())

	// Re-open one more time and re-register chains
	config.DB = versiondb.New(baseDB)
	idxrIntf, err = NewIndexer(config)
	assert.NoError(err)
	idxr, ok = idxrIntf.(*indexer)
	assert.True(ok)
	idxr.RegisterChain("chain1", chain1Ctx, chainEngine)
	idxr.RegisterChain("chain2", chain2Ctx, dagEngine)

	// Verify state
	lastAcceptedTx, err = idxr.txIndices[chain2Ctx.ChainID].GetLastAccepted()
	assert.NoError(err)
	assert.EqualValues(txID, lastAcceptedTx.ID)
	lastAcceptedVtx, err = idxr.vtxIndices[chain2Ctx.ChainID].GetLastAccepted()
	assert.NoError(err)
	assert.EqualValues(vtxID, lastAcceptedVtx.ID)
	lastAcceptedBlk, err = idxr.blockIndices[chain1Ctx.ChainID].GetLastAccepted()
	assert.NoError(err)
	assert.EqualValues(blkID, lastAcceptedBlk.ID)
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
