// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package indexer

import (
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/api/server"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex/vertexmock"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blockmock"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var (
	_ server.PathAdder = (*apiServerMock)(nil)

	errUnimplemented = errors.New("unimplemented")
)

type apiServerMock struct {
	timesCalled int
	endpoints   []string
}

func (a *apiServerMock) AddRoute(_ http.Handler, endpoint, _ string) error {
	a.timesCalled++
	a.endpoints = append(a.endpoints, endpoint)
	return nil
}

func (*apiServerMock) AddAliases(string, ...string) error {
	return errUnimplemented
}

// Test that newIndexer sets fields correctly
func TestNewIndexer(t *testing.T) {
	require := require.New(t)
	config := Config{
		IndexingEnabled:      true,
		AllowIncompleteIndex: true,
		Log:                  logging.NoLog{},
		DB:                   memdb.New(),
		BlockAcceptorGroup:   snow.NewAcceptorGroup(logging.NoLog{}),
		TxAcceptorGroup:      snow.NewAcceptorGroup(logging.NoLog{}),
		VertexAcceptorGroup:  snow.NewAcceptorGroup(logging.NoLog{}),
		APIServer:            &apiServerMock{},
		ShutdownF:            func() {},
	}

	idxrIntf, err := NewIndexer(config)
	require.NoError(err)
	require.IsType(&indexer{}, idxrIntf)
	idxr := idxrIntf.(*indexer)
	require.NotNil(idxr.log)
	require.NotNil(idxr.db)
	require.False(idxr.closed)
	require.NotNil(idxr.pathAdder)
	require.True(idxr.indexingEnabled)
	require.True(idxr.allowIncompleteIndex)
	require.NotNil(idxr.blockIndices)
	require.Empty(idxr.blockIndices)
	require.NotNil(idxr.txIndices)
	require.Empty(idxr.txIndices)
	require.NotNil(idxr.vtxIndices)
	require.Empty(idxr.vtxIndices)
	require.NotNil(idxr.blockAcceptorGroup)
	require.NotNil(idxr.txAcceptorGroup)
	require.NotNil(idxr.vertexAcceptorGroup)
	require.NotNil(idxr.shutdownF)
	require.False(idxr.hasRunBefore)
}

// Test that [hasRunBefore] is set correctly and that Shutdown is called on close
func TestMarkHasRunAndShutdown(t *testing.T) {
	require := require.New(t)
	baseDB := memdb.New()
	db := versiondb.New(baseDB)
	shutdown := &sync.WaitGroup{}
	shutdown.Add(1)
	config := Config{
		IndexingEnabled:     true,
		Log:                 logging.NoLog{},
		DB:                  db,
		BlockAcceptorGroup:  snow.NewAcceptorGroup(logging.NoLog{}),
		TxAcceptorGroup:     snow.NewAcceptorGroup(logging.NoLog{}),
		VertexAcceptorGroup: snow.NewAcceptorGroup(logging.NoLog{}),
		APIServer:           &apiServerMock{},
		ShutdownF:           shutdown.Done,
	}

	idxrIntf, err := NewIndexer(config)
	require.NoError(err)
	require.False(idxrIntf.(*indexer).hasRunBefore)
	require.NoError(db.Commit())
	require.NoError(idxrIntf.Close())
	shutdown.Wait()
	shutdown.Add(1)

	config.DB = versiondb.New(baseDB)
	idxrIntf, err = NewIndexer(config)
	require.NoError(err)
	require.IsType(&indexer{}, idxrIntf)
	idxr := idxrIntf.(*indexer)
	require.True(idxr.hasRunBefore)
	require.NoError(idxr.Close())
	shutdown.Wait()
}

// Test registering a linear chain and a DAG chain and accepting
// some vertices
func TestIndexer(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	baseDB := memdb.New()
	db := versiondb.New(baseDB)
	server := &apiServerMock{}
	config := Config{
		IndexingEnabled:      true,
		AllowIncompleteIndex: false,
		Log:                  logging.NoLog{},
		DB:                   db,
		BlockAcceptorGroup:   snow.NewAcceptorGroup(logging.NoLog{}),
		TxAcceptorGroup:      snow.NewAcceptorGroup(logging.NoLog{}),
		VertexAcceptorGroup:  snow.NewAcceptorGroup(logging.NoLog{}),
		APIServer:            server,
		ShutdownF:            func() {},
	}

	// Create indexer
	idxrIntf, err := NewIndexer(config)
	require.NoError(err)
	require.IsType(&indexer{}, idxrIntf)
	idxr := idxrIntf.(*indexer)
	now := time.Now()
	idxr.clock.Set(now)

	// Assert state is right
	snow1Ctx := snowtest.Context(t, snowtest.CChainID)
	chain1Ctx := snowtest.ConsensusContext(snow1Ctx)
	isIncomplete, err := idxr.isIncomplete(chain1Ctx.ChainID)
	require.NoError(err)
	require.False(isIncomplete)
	previouslyIndexed, err := idxr.previouslyIndexed(chain1Ctx.ChainID)
	require.NoError(err)
	require.False(previouslyIndexed)

	// Register this chain, creating a new index
	chainVM := blockmock.NewChainVM(ctrl)
	idxr.RegisterChain("chain1", chain1Ctx, chainVM)
	isIncomplete, err = idxr.isIncomplete(chain1Ctx.ChainID)
	require.NoError(err)
	require.False(isIncomplete)
	previouslyIndexed, err = idxr.previouslyIndexed(chain1Ctx.ChainID)
	require.NoError(err)
	require.True(previouslyIndexed)
	require.Equal(1, server.timesCalled)
	require.Equal("index/chain1/block", server.endpoints[0])
	require.Len(idxr.blockIndices, 1)
	require.Empty(idxr.txIndices)
	require.Empty(idxr.vtxIndices)

	// Accept a container
	blkID, blkBytes := ids.GenerateTestID(), utils.RandomBytes(32)
	expectedContainer := Container{
		ID:        blkID,
		Bytes:     blkBytes,
		Timestamp: now.UnixNano(),
	}

	require.NoError(config.BlockAcceptorGroup.Accept(chain1Ctx, blkID, blkBytes))

	blkIdx := idxr.blockIndices[chain1Ctx.ChainID]
	require.NotNil(blkIdx)

	// Verify GetLastAccepted is right
	gotLastAccepted, err := blkIdx.GetLastAccepted()
	require.NoError(err)
	require.Equal(expectedContainer, gotLastAccepted)

	// Verify GetContainerByID is right
	container, err := blkIdx.GetContainerByID(blkID)
	require.NoError(err)
	require.Equal(expectedContainer, container)

	// Verify GetIndex is right
	index, err := blkIdx.GetIndex(blkID)
	require.NoError(err)
	require.Zero(index)

	// Verify GetContainerByIndex is right
	container, err = blkIdx.GetContainerByIndex(0)
	require.NoError(err)
	require.Equal(expectedContainer, container)

	// Verify GetContainerRange is right
	containers, err := blkIdx.GetContainerRange(0, 1)
	require.NoError(err)
	require.Len(containers, 1)
	require.Equal(expectedContainer, containers[0])

	// Close the indexer
	require.NoError(db.Commit())
	require.NoError(idxr.Close())
	require.True(idxr.closed)
	// Calling Close again should be fine
	require.NoError(idxr.Close())
	server.timesCalled = 0

	// Re-open the indexer
	config.DB = versiondb.New(baseDB)
	idxrIntf, err = NewIndexer(config)
	require.NoError(err)
	require.IsType(&indexer{}, idxrIntf)
	idxr = idxrIntf.(*indexer)
	now = time.Now()
	idxr.clock.Set(now)
	require.Empty(idxr.blockIndices)
	require.Empty(idxr.txIndices)
	require.Empty(idxr.vtxIndices)
	require.True(idxr.hasRunBefore)
	previouslyIndexed, err = idxr.previouslyIndexed(chain1Ctx.ChainID)
	require.NoError(err)
	require.True(previouslyIndexed)
	hasRun, err := idxr.hasRun()
	require.NoError(err)
	require.True(hasRun)
	isIncomplete, err = idxr.isIncomplete(chain1Ctx.ChainID)
	require.NoError(err)
	require.False(isIncomplete)

	// Register the same chain as before
	idxr.RegisterChain("chain1", chain1Ctx, chainVM)
	blkIdx = idxr.blockIndices[chain1Ctx.ChainID]
	require.NotNil(blkIdx)
	container, err = blkIdx.GetLastAccepted()
	require.NoError(err)
	require.Equal(blkID, container.ID)
	require.Equal(1, server.timesCalled) // block index for chain
	require.Contains(server.endpoints, "index/chain1/block")

	// Register a DAG chain
	snow2Ctx := snowtest.Context(t, snowtest.XChainID)
	chain2Ctx := snowtest.ConsensusContext(snow2Ctx)
	isIncomplete, err = idxr.isIncomplete(chain2Ctx.ChainID)
	require.NoError(err)
	require.False(isIncomplete)
	previouslyIndexed, err = idxr.previouslyIndexed(chain2Ctx.ChainID)
	require.NoError(err)
	require.False(previouslyIndexed)
	dagVM := vertexmock.NewLinearizableVM(ctrl)
	idxr.RegisterChain("chain2", chain2Ctx, dagVM)
	require.NoError(err)
	require.Equal(4, server.timesCalled) // block index for chain, block index for dag, vtx index, tx index
	require.Contains(server.endpoints, "index/chain2/block")
	require.Contains(server.endpoints, "index/chain2/vtx")
	require.Contains(server.endpoints, "index/chain2/tx")
	require.Len(idxr.blockIndices, 2)
	require.Len(idxr.txIndices, 1)
	require.Len(idxr.vtxIndices, 1)

	// Accept a vertex
	vtxID, vtxBytes := ids.GenerateTestID(), utils.RandomBytes(32)
	expectedVtx := Container{
		ID:        vtxID,
		Bytes:     vtxBytes,
		Timestamp: now.UnixNano(),
	}

	require.NoError(config.VertexAcceptorGroup.Accept(chain2Ctx, vtxID, vtxBytes))

	vtxIdx := idxr.vtxIndices[chain2Ctx.ChainID]
	require.NotNil(vtxIdx)

	// Verify GetLastAccepted is right
	gotLastAccepted, err = vtxIdx.GetLastAccepted()
	require.NoError(err)
	require.Equal(expectedVtx, gotLastAccepted)

	// Verify GetContainerByID is right
	vtx, err := vtxIdx.GetContainerByID(vtxID)
	require.NoError(err)
	require.Equal(expectedVtx, vtx)

	// Verify GetIndex is right
	index, err = vtxIdx.GetIndex(vtxID)
	require.NoError(err)
	require.Zero(index)

	// Verify GetContainerByIndex is right
	vtx, err = vtxIdx.GetContainerByIndex(0)
	require.NoError(err)
	require.Equal(expectedVtx, vtx)

	// Verify GetContainerRange is right
	vtxs, err := vtxIdx.GetContainerRange(0, 1)
	require.NoError(err)
	require.Len(vtxs, 1)
	require.Equal(expectedVtx, vtxs[0])

	// Accept a tx
	txID, txBytes := ids.GenerateTestID(), utils.RandomBytes(32)
	expectedTx := Container{
		ID:        txID,
		Bytes:     txBytes,
		Timestamp: now.UnixNano(),
	}

	require.NoError(config.TxAcceptorGroup.Accept(chain2Ctx, txID, txBytes))

	txIdx := idxr.txIndices[chain2Ctx.ChainID]
	require.NotNil(txIdx)

	// Verify GetLastAccepted is right
	gotLastAccepted, err = txIdx.GetLastAccepted()
	require.NoError(err)
	require.Equal(expectedTx, gotLastAccepted)

	// Verify GetContainerByID is right
	tx, err := txIdx.GetContainerByID(txID)
	require.NoError(err)
	require.Equal(expectedTx, tx)

	// Verify GetIndex is right
	index, err = txIdx.GetIndex(txID)
	require.NoError(err)
	require.Zero(index)

	// Verify GetContainerByIndex is right
	tx, err = txIdx.GetContainerByIndex(0)
	require.NoError(err)
	require.Equal(expectedTx, tx)

	// Verify GetContainerRange is right
	txs, err := txIdx.GetContainerRange(0, 1)
	require.NoError(err)
	require.Len(txs, 1)
	require.Equal(expectedTx, txs[0])

	// Accepting a vertex shouldn't have caused anything to
	// happen on the block/tx index. Similar for tx.
	lastAcceptedTx, err := txIdx.GetLastAccepted()
	require.NoError(err)
	require.Equal(txID, lastAcceptedTx.ID)
	lastAcceptedVtx, err := vtxIdx.GetLastAccepted()
	require.NoError(err)
	require.Equal(vtxID, lastAcceptedVtx.ID)
	lastAcceptedBlk, err := blkIdx.GetLastAccepted()
	require.NoError(err)
	require.Equal(blkID, lastAcceptedBlk.ID)

	// Close the indexer again
	require.NoError(config.DB.(*versiondb.Database).Commit())
	require.NoError(idxr.Close())

	// Re-open one more time and re-register chains
	config.DB = versiondb.New(baseDB)
	idxrIntf, err = NewIndexer(config)
	require.NoError(err)
	require.IsType(&indexer{}, idxrIntf)
	idxr = idxrIntf.(*indexer)
	idxr.RegisterChain("chain1", chain1Ctx, chainVM)
	idxr.RegisterChain("chain2", chain2Ctx, dagVM)

	// Verify state
	lastAcceptedTx, err = idxr.txIndices[chain2Ctx.ChainID].GetLastAccepted()
	require.NoError(err)
	require.Equal(txID, lastAcceptedTx.ID)
	lastAcceptedVtx, err = idxr.vtxIndices[chain2Ctx.ChainID].GetLastAccepted()
	require.NoError(err)
	require.Equal(vtxID, lastAcceptedVtx.ID)
	lastAcceptedBlk, err = idxr.blockIndices[chain1Ctx.ChainID].GetLastAccepted()
	require.NoError(err)
	require.Equal(blkID, lastAcceptedBlk.ID)
}

// Make sure the indexer doesn't allow incomplete indices unless explicitly allowed
func TestIncompleteIndex(t *testing.T) {
	// Create an indexer with indexing disabled
	require := require.New(t)
	ctrl := gomock.NewController(t)

	baseDB := memdb.New()
	config := Config{
		IndexingEnabled:      false,
		AllowIncompleteIndex: false,
		Log:                  logging.NoLog{},
		DB:                   versiondb.New(baseDB),
		BlockAcceptorGroup:   snow.NewAcceptorGroup(logging.NoLog{}),
		TxAcceptorGroup:      snow.NewAcceptorGroup(logging.NoLog{}),
		VertexAcceptorGroup:  snow.NewAcceptorGroup(logging.NoLog{}),
		APIServer:            &apiServerMock{},
		ShutdownF:            func() {},
	}
	idxrIntf, err := NewIndexer(config)
	require.NoError(err)
	require.IsType(&indexer{}, idxrIntf)
	idxr := idxrIntf.(*indexer)
	require.False(idxr.indexingEnabled)

	// Register a chain
	snow1Ctx := snowtest.Context(t, snowtest.CChainID)
	chain1Ctx := snowtest.ConsensusContext(snow1Ctx)
	isIncomplete, err := idxr.isIncomplete(chain1Ctx.ChainID)
	require.NoError(err)
	require.False(isIncomplete)
	previouslyIndexed, err := idxr.previouslyIndexed(chain1Ctx.ChainID)
	require.NoError(err)
	require.False(previouslyIndexed)
	chainVM := blockmock.NewChainVM(ctrl)
	idxr.RegisterChain("chain1", chain1Ctx, chainVM)
	isIncomplete, err = idxr.isIncomplete(chain1Ctx.ChainID)
	require.NoError(err)
	require.True(isIncomplete)
	require.Empty(idxr.blockIndices)

	// Close and re-open the indexer, this time with indexing enabled
	require.NoError(config.DB.(*versiondb.Database).Commit())
	require.NoError(idxr.Close())
	config.IndexingEnabled = true
	config.DB = versiondb.New(baseDB)
	idxrIntf, err = NewIndexer(config)
	require.NoError(err)
	require.IsType(&indexer{}, idxrIntf)
	idxr = idxrIntf.(*indexer)
	require.True(idxr.indexingEnabled)

	// Register the chain again. Should die due to incomplete index.
	require.NoError(config.DB.(*versiondb.Database).Commit())
	idxr.RegisterChain("chain1", chain1Ctx, chainVM)
	require.True(idxr.closed)

	// Close and re-open the indexer, this time with indexing enabled
	// and incomplete index allowed.
	require.NoError(idxr.Close())
	config.AllowIncompleteIndex = true
	config.DB = versiondb.New(baseDB)
	idxrIntf, err = NewIndexer(config)
	require.NoError(err)
	require.IsType(&indexer{}, idxrIntf)
	idxr = idxrIntf.(*indexer)
	require.True(idxr.allowIncompleteIndex)

	// Register the chain again. Should be OK
	idxr.RegisterChain("chain1", chain1Ctx, chainVM)
	require.False(idxr.closed)

	// Close the indexer and re-open with indexing disabled and
	// incomplete index not allowed.
	require.NoError(idxr.Close())
	config.AllowIncompleteIndex = false
	config.IndexingEnabled = false
	config.DB = versiondb.New(baseDB)
	idxrIntf, err = NewIndexer(config)
	require.NoError(err)
	require.IsType(&indexer{}, idxrIntf)
}

// Ensure we only index chains in the primary network
func TestIgnoreNonDefaultChains(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	baseDB := memdb.New()
	db := versiondb.New(baseDB)
	config := Config{
		IndexingEnabled:      true,
		AllowIncompleteIndex: false,
		Log:                  logging.NoLog{},
		DB:                   db,
		BlockAcceptorGroup:   snow.NewAcceptorGroup(logging.NoLog{}),
		TxAcceptorGroup:      snow.NewAcceptorGroup(logging.NoLog{}),
		VertexAcceptorGroup:  snow.NewAcceptorGroup(logging.NoLog{}),
		APIServer:            &apiServerMock{},
		ShutdownF:            func() {},
	}

	// Create indexer
	idxrIntf, err := NewIndexer(config)
	require.NoError(err)
	require.IsType(&indexer{}, idxrIntf)
	idxr := idxrIntf.(*indexer)

	// Create chain1Ctx for a random subnet + chain.
	chain1Ctx := snowtest.ConsensusContext(&snow.Context{
		ChainID:  ids.GenerateTestID(),
		SubnetID: ids.GenerateTestID(),
	})

	// RegisterChain should return without adding an index for this chain
	chainVM := blockmock.NewChainVM(ctrl)
	idxr.RegisterChain("chain1", chain1Ctx, chainVM)
	require.Empty(idxr.blockIndices)
}
