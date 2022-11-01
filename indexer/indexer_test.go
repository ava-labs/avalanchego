// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package indexer

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/server"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"

	aveng "github.com/ava-labs/avalanchego/snow/engine/avalanche"
	smblockmocks "github.com/ava-labs/avalanchego/snow/engine/snowman/block/mocks"
)

var _ server.PathAdder = (*apiServerMock)(nil)

type apiServerMock struct {
	timesCalled int
	bases       []string
	endpoints   []string
}

func (a *apiServerMock) AddRoute(_ *common.HTTPHandler, _ *sync.RWMutex, base, endpoint string) error {
	a.timesCalled++
	a.bases = append(a.bases, base)
	a.endpoints = append(a.endpoints, endpoint)
	return nil
}

func (a *apiServerMock) AddAliases(string, ...string) error {
	return errors.New("unimplemented")
}

// Test that newIndexer sets fields correctly
func TestNewIndexer(t *testing.T) {
	require := require.New(t)
	config := Config{
		IndexingEnabled:        true,
		AllowIncompleteIndex:   true,
		Log:                    logging.NoLog{},
		DB:                     memdb.New(),
		DecisionAcceptorGroup:  snow.NewAcceptorGroup(logging.NoLog{}),
		ConsensusAcceptorGroup: snow.NewAcceptorGroup(logging.NoLog{}),
		APIServer:              &apiServerMock{},
		ShutdownF:              func() {},
	}

	idxrIntf, err := NewIndexer(config)
	require.NoError(err)
	idxr, ok := idxrIntf.(*indexer)
	require.True(ok)
	require.NotNil(idxr.codec)
	require.NotNil(idxr.log)
	require.NotNil(idxr.db)
	require.False(idxr.closed)
	require.NotNil(idxr.pathAdder)
	require.True(idxr.indexingEnabled)
	require.True(idxr.allowIncompleteIndex)
	require.NotNil(idxr.blockIndices)
	require.Len(idxr.blockIndices, 0)
	require.NotNil(idxr.txIndices)
	require.Len(idxr.txIndices, 0)
	require.NotNil(idxr.vtxIndices)
	require.Len(idxr.vtxIndices, 0)
	require.NotNil(idxr.consensusAcceptorGroup)
	require.NotNil(idxr.decisionAcceptorGroup)
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
		IndexingEnabled:        true,
		Log:                    logging.NoLog{},
		DB:                     db,
		DecisionAcceptorGroup:  snow.NewAcceptorGroup(logging.NoLog{}),
		ConsensusAcceptorGroup: snow.NewAcceptorGroup(logging.NoLog{}),
		APIServer:              &apiServerMock{},
		ShutdownF:              func() { shutdown.Done() },
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
	idxr, ok := idxrIntf.(*indexer)
	require.True(ok)
	require.True(idxr.hasRunBefore)
	require.NoError(idxr.Close())
	shutdown.Wait()
}

// Test registering a linear chain and a DAG chain and accepting
// some vertices
func TestIndexer(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	baseDB := memdb.New()
	db := versiondb.New(baseDB)
	config := Config{
		IndexingEnabled:        true,
		AllowIncompleteIndex:   false,
		Log:                    logging.NoLog{},
		DB:                     db,
		DecisionAcceptorGroup:  snow.NewAcceptorGroup(logging.NoLog{}),
		ConsensusAcceptorGroup: snow.NewAcceptorGroup(logging.NoLog{}),
		APIServer:              &apiServerMock{},
		ShutdownF:              func() {},
	}

	// Create indexer
	idxrIntf, err := NewIndexer(config)
	require.NoError(err)
	idxr, ok := idxrIntf.(*indexer)
	require.True(ok)
	now := time.Now()
	idxr.clock.Set(now)

	// Assert state is right
	chain1Ctx := snow.DefaultConsensusContextTest()
	chain1Ctx.ChainID = ids.GenerateTestID()
	isIncomplete, err := idxr.isIncomplete(chain1Ctx.ChainID)
	require.NoError(err)
	require.False(isIncomplete)
	previouslyIndexed, err := idxr.previouslyIndexed(chain1Ctx.ChainID)
	require.NoError(err)
	require.False(previouslyIndexed)

	// Register this chain, creating a new index
	chainVM := smblockmocks.NewMockChainVM(ctrl)
	chainEngine := snowman.NewMockEngine(ctrl)
	chainEngine.EXPECT().Context().AnyTimes().Return(chain1Ctx)
	chainEngine.EXPECT().GetVM().AnyTimes().Return(chainVM)

	idxr.RegisterChain("chain1", chainEngine)
	isIncomplete, err = idxr.isIncomplete(chain1Ctx.ChainID)
	require.NoError(err)
	require.False(isIncomplete)
	previouslyIndexed, err = idxr.previouslyIndexed(chain1Ctx.ChainID)
	require.NoError(err)
	require.True(previouslyIndexed)
	server := config.APIServer.(*apiServerMock)
	require.EqualValues(1, server.timesCalled)
	require.EqualValues("index/chain1", server.bases[0])
	require.EqualValues("/block", server.endpoints[0])
	require.Len(idxr.blockIndices, 1)
	require.Len(idxr.txIndices, 0)
	require.Len(idxr.vtxIndices, 0)

	// Accept a container
	blkID, blkBytes := ids.GenerateTestID(), utils.RandomBytes(32)
	expectedContainer := Container{
		ID:        blkID,
		Bytes:     blkBytes,
		Timestamp: now.UnixNano(),
	}

	require.NoError(config.ConsensusAcceptorGroup.Accept(chain1Ctx, blkID, blkBytes))

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
	require.EqualValues(0, index)

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
	idxr, ok = idxrIntf.(*indexer)
	now = time.Now()
	idxr.clock.Set(now)
	require.True(ok)
	require.Len(idxr.blockIndices, 0)
	require.Len(idxr.txIndices, 0)
	require.Len(idxr.vtxIndices, 0)
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
	idxr.RegisterChain("chain1", chainEngine)
	blkIdx = idxr.blockIndices[chain1Ctx.ChainID]
	require.NotNil(blkIdx)
	container, err = blkIdx.GetLastAccepted()
	require.NoError(err)
	require.Equal(blkID, container.ID)

	// Register a DAG chain
	chain2Ctx := snow.DefaultConsensusContextTest()
	chain2Ctx.ChainID = ids.GenerateTestID()
	isIncomplete, err = idxr.isIncomplete(chain2Ctx.ChainID)
	require.NoError(err)
	require.False(isIncomplete)
	previouslyIndexed, err = idxr.previouslyIndexed(chain2Ctx.ChainID)
	require.NoError(err)
	require.False(previouslyIndexed)
	dagVM := vertex.NewMockDAGVM(ctrl)
	dagEngine := aveng.NewMockEngine(ctrl)
	dagEngine.EXPECT().Context().AnyTimes().Return(chain2Ctx)
	dagEngine.EXPECT().GetVM().AnyTimes().Return(dagVM)
	idxr.RegisterChain("chain2", dagEngine)
	require.NoError(err)
	server = config.APIServer.(*apiServerMock)
	require.EqualValues(3, server.timesCalled) // block index, vtx index, tx index
	require.Contains(server.bases, "index/chain2")
	require.Contains(server.endpoints, "/vtx")
	require.Contains(server.endpoints, "/tx")
	require.Len(idxr.blockIndices, 1)
	require.Len(idxr.txIndices, 1)
	require.Len(idxr.vtxIndices, 1)

	// Accept a vertex
	vtxID, vtxBytes := ids.GenerateTestID(), utils.RandomBytes(32)
	expectedVtx := Container{
		ID:        vtxID,
		Bytes:     blkBytes,
		Timestamp: now.UnixNano(),
	}
	// Mocked VM knows about this block now
	dagEngine.EXPECT().GetVtx(vtxID).Return(
		&avalanche.TestVertex{
			TestDecidable: choices.TestDecidable{
				StatusV: choices.Accepted,
				IDV:     vtxID,
			},
			BytesV: vtxBytes,
		}, nil,
	).AnyTimes()

	require.NoError(config.ConsensusAcceptorGroup.Accept(chain2Ctx, vtxID, blkBytes))

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
	require.EqualValues(0, index)

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
		Bytes:     blkBytes,
		Timestamp: now.UnixNano(),
	}
	// Mocked VM knows about this tx now
	dagVM.EXPECT().GetTx(txID).Return(
		&snowstorm.TestTx{
			TestDecidable: choices.TestDecidable{
				IDV:     txID,
				StatusV: choices.Accepted,
			},
			BytesV: txBytes,
		}, nil,
	).AnyTimes()

	require.NoError(config.DecisionAcceptorGroup.Accept(chain2Ctx, txID, blkBytes))

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
	require.EqualValues(0, index)

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
	require.EqualValues(txID, lastAcceptedTx.ID)
	lastAcceptedVtx, err := vtxIdx.GetLastAccepted()
	require.NoError(err)
	require.EqualValues(vtxID, lastAcceptedVtx.ID)
	lastAcceptedBlk, err := blkIdx.GetLastAccepted()
	require.NoError(err)
	require.EqualValues(blkID, lastAcceptedBlk.ID)

	// Close the indexer again
	require.NoError(config.DB.(*versiondb.Database).Commit())
	require.NoError(idxr.Close())

	// Re-open one more time and re-register chains
	config.DB = versiondb.New(baseDB)
	idxrIntf, err = NewIndexer(config)
	require.NoError(err)
	idxr, ok = idxrIntf.(*indexer)
	require.True(ok)
	idxr.RegisterChain("chain1", chainEngine)
	idxr.RegisterChain("chain2", dagEngine)

	// Verify state
	lastAcceptedTx, err = idxr.txIndices[chain2Ctx.ChainID].GetLastAccepted()
	require.NoError(err)
	require.EqualValues(txID, lastAcceptedTx.ID)
	lastAcceptedVtx, err = idxr.vtxIndices[chain2Ctx.ChainID].GetLastAccepted()
	require.NoError(err)
	require.EqualValues(vtxID, lastAcceptedVtx.ID)
	lastAcceptedBlk, err = idxr.blockIndices[chain1Ctx.ChainID].GetLastAccepted()
	require.NoError(err)
	require.EqualValues(blkID, lastAcceptedBlk.ID)
}

// Make sure the indexer doesn't allow incomplete indices unless explicitly allowed
func TestIncompleteIndex(t *testing.T) {
	// Create an indexer with indexing disabled
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	baseDB := memdb.New()
	config := Config{
		IndexingEnabled:        false,
		AllowIncompleteIndex:   false,
		Log:                    logging.NoLog{},
		DB:                     versiondb.New(baseDB),
		DecisionAcceptorGroup:  snow.NewAcceptorGroup(logging.NoLog{}),
		ConsensusAcceptorGroup: snow.NewAcceptorGroup(logging.NoLog{}),
		APIServer:              &apiServerMock{},
		ShutdownF:              func() {},
	}
	idxrIntf, err := NewIndexer(config)
	require.NoError(err)
	idxr, ok := idxrIntf.(*indexer)
	require.True(ok)
	require.False(idxr.indexingEnabled)

	// Register a chain
	chain1Ctx := snow.DefaultConsensusContextTest()
	chain1Ctx.ChainID = ids.GenerateTestID()
	isIncomplete, err := idxr.isIncomplete(chain1Ctx.ChainID)
	require.NoError(err)
	require.False(isIncomplete)
	previouslyIndexed, err := idxr.previouslyIndexed(chain1Ctx.ChainID)
	require.NoError(err)
	require.False(previouslyIndexed)
	chainEngine := snowman.NewMockEngine(ctrl)
	chainEngine.EXPECT().Context().AnyTimes().Return(chain1Ctx)
	idxr.RegisterChain("chain1", chainEngine)
	isIncomplete, err = idxr.isIncomplete(chain1Ctx.ChainID)
	require.NoError(err)
	require.True(isIncomplete)
	require.Len(idxr.blockIndices, 0)

	// Close and re-open the indexer, this time with indexing enabled
	require.NoError(config.DB.(*versiondb.Database).Commit())
	require.NoError(idxr.Close())
	config.IndexingEnabled = true
	config.DB = versiondb.New(baseDB)
	idxrIntf, err = NewIndexer(config)
	require.NoError(err)
	idxr, ok = idxrIntf.(*indexer)
	require.True(ok)
	require.True(idxr.indexingEnabled)

	// Register the chain again. Should die due to incomplete index.
	require.NoError(config.DB.(*versiondb.Database).Commit())
	idxr.RegisterChain("chain1", chainEngine)
	require.True(idxr.closed)

	// Close and re-open the indexer, this time with indexing enabled
	// and incomplete index allowed.
	require.NoError(idxr.Close())
	config.AllowIncompleteIndex = true
	config.DB = versiondb.New(baseDB)
	idxrIntf, err = NewIndexer(config)
	require.NoError(err)
	idxr, ok = idxrIntf.(*indexer)
	require.True(ok)
	require.True(idxr.allowIncompleteIndex)

	// Register the chain again. Should be OK
	idxr.RegisterChain("chain1", chainEngine)
	require.False(idxr.closed)

	// Close the indexer and re-open with indexing disabled and
	// incomplete index not allowed.
	require.NoError(idxr.Close())
	config.AllowIncompleteIndex = false
	config.IndexingEnabled = false
	config.DB = versiondb.New(baseDB)
	idxrIntf, err = NewIndexer(config)
	require.NoError(err)
	_, ok = idxrIntf.(*indexer)
	require.True(ok)
}

// Ensure we only index chains in the primary network
func TestIgnoreNonDefaultChains(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	baseDB := memdb.New()
	db := versiondb.New(baseDB)
	config := Config{
		IndexingEnabled:        true,
		AllowIncompleteIndex:   false,
		Log:                    logging.NoLog{},
		DB:                     db,
		DecisionAcceptorGroup:  snow.NewAcceptorGroup(logging.NoLog{}),
		ConsensusAcceptorGroup: snow.NewAcceptorGroup(logging.NoLog{}),
		APIServer:              &apiServerMock{},
		ShutdownF:              func() {},
	}

	// Create indexer
	idxrIntf, err := NewIndexer(config)
	require.NoError(err)
	idxr, ok := idxrIntf.(*indexer)
	require.True(ok)

	// Assert state is right
	chain1Ctx := snow.DefaultConsensusContextTest()
	chain1Ctx.ChainID = ids.GenerateTestID()
	chain1Ctx.SubnetID = ids.GenerateTestID()

	// RegisterChain should return without adding an index for this chain
	chainVM := smblockmocks.NewMockChainVM(ctrl)
	chainEngine := snowman.NewMockEngine(ctrl)
	chainEngine.EXPECT().Context().AnyTimes().Return(chain1Ctx)
	chainEngine.EXPECT().GetVM().AnyTimes().Return(chainVM)
	idxr.RegisterChain("chain1", chainEngine)
	require.Len(idxr.blockIndices, 0)
}
