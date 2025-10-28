// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"math/big"
	"os"
	"slices"
	"testing"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/utils/logging"

	heightindexdb "github.com/ava-labs/avalanchego/x/blockdb"
	evmdb "github.com/ava-labs/coreth/plugin/evm/database"
)

func TestMain(m *testing.M) {
	customtypes.Register()
	params.RegisterExtras()
	os.Exit(m.Run())
}

func TestDatabaseWriteReadBlock(t *testing.T) {
	dataDir := t.TempDir()
	db, _ := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 10)
	writeBlocks(db, blocks, receipts)

	for _, block := range blocks {
		actualBlock := rawdb.ReadBlock(db, block.Hash(), block.NumberU64())
		requireRLPEqual(t, block, actualBlock)
	}
}

func TestDatabaseWriteReadReceipts(t *testing.T) {
	dataDir := t.TempDir()
	db, _ := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 10)
	writeBlocks(db, blocks, receipts)

	for i, block := range blocks {
		require.True(t, rawdb.HasReceipts(db, block.Hash(), block.NumberU64()))
		actualReceipts := rawdb.ReadReceipts(
			db, block.Hash(), block.NumberU64(), block.Time(), params.TestChainConfig,
		)
		requireRLPEqual(t, receipts[i], actualReceipts)
	}
}

func TestDatabaseReadLogs(t *testing.T) {
	dataDir := t.TempDir()
	db, _ := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 10)
	writeBlocks(db, blocks, receipts)

	for i, block := range blocks {
		actualLogs := rawdb.ReadLogs(db, block.Hash(), block.NumberU64())
		blockReceipts := receipts[i]
		requireRLPEqual(t, logsFromReceipts(blockReceipts), actualLogs)
	}
}

func TestDatabaseDeleteNoOp(t *testing.T) {
	// Verifies that block header, body and receipts cannot be deleted (no-op),
	// but hash to height mapping should be deleted.
	tests := []struct {
		name      string
		useBatch  bool
		batchSize int
	}{
		{
			name:      "db delete",
			useBatch:  false,
			batchSize: 0,
		},
		{
			name:      "batch delete",
			useBatch:  true,
			batchSize: 0,
		},
		{
			name:      "batch delete with size",
			useBatch:  true,
			batchSize: 1024,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			db, _ := newDatabasesFromDir(t, dataDir)
			allBlocks, allReceipts := createBlocks(t, 4)
			blocks := allBlocks[1:]
			receipts := allReceipts[1:]
			writeBlocks(db, blocks, receipts)

			// Perform delete operations on all blocks
			if tc.useBatch {
				var batch ethdb.Batch
				if tc.batchSize > 0 {
					batch = db.NewBatchWithSize(tc.batchSize)
				} else {
					batch = db.NewBatch()
				}

				for _, block := range blocks {
					rawdb.DeleteBlock(batch, block.Hash(), block.NumberU64())
				}
				require.NoError(t, batch.Write())
			} else {
				for _, block := range blocks {
					rawdb.DeleteBlock(db, block.Hash(), block.NumberU64())
				}
			}

			// Verify blocks still exist
			for i, block := range blocks {
				actualBlock := rawdb.ReadBlock(db, block.Hash(), block.NumberU64())
				requireRLPEqual(t, block, actualBlock)
				require.True(t, rawdb.HasReceipts(db, block.Hash(), block.NumberU64()))
				expReceipts := receipts[i]
				logs := rawdb.ReadLogs(db, block.Hash(), block.NumberU64())
				requireRLPEqual(t, logsFromReceipts(expReceipts), logs)
			}
		})
	}
}

func TestDatabaseWriteToHeightDB(t *testing.T) {
	dataDir := t.TempDir()
	db, kvDB := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 2)
	writeBlocks(db, blocks, receipts)

	heightBlock := blocks[1]

	// Verify no block data in kvDB
	require.False(t, rawdb.HasHeader(kvDB, heightBlock.Hash(), heightBlock.NumberU64()))
	require.False(t, rawdb.HasBody(kvDB, heightBlock.Hash(), heightBlock.NumberU64()))
	require.False(t, rawdb.HasReceipts(kvDB, heightBlock.Hash(), heightBlock.NumberU64()))

	// Verify block data in height-indexed databases
	ok, err := db.headerDB.Has(heightBlock.NumberU64())
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = db.bodyDB.Has(heightBlock.NumberU64())
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = db.receiptsDB.Has(heightBlock.NumberU64())
	require.NoError(t, err)
	require.True(t, ok)
}

func TestDatabaseNewBatch(t *testing.T) {
	dataDir := t.TempDir()
	db, _ := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 2)
	batch := db.NewBatch()
	writeBlocks(batch, blocks, receipts)

	// After adding blocks to batch, blocks and receipts should be available immediately
	require.True(t, rawdb.HasBody(db, blocks[1].Hash(), blocks[1].NumberU64()))
	require.True(t, rawdb.HasHeader(db, blocks[1].Hash(), blocks[1].NumberU64()))
	require.True(t, rawdb.HasReceipts(db, blocks[1].Hash(), blocks[1].NumberU64()))

	// Header number should not be available until batch is written
	require.Nil(t, rawdb.ReadHeaderNumber(db, blocks[1].Hash()))
	require.NoError(t, batch.Write())
	num := rawdb.ReadHeaderNumber(db, blocks[1].Hash())
	require.Equal(t, blocks[1].NumberU64(), *num)
}

func TestDatabaseNewBatchWithSize(t *testing.T) {
	dataDir := t.TempDir()
	db, _ := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 2)
	batch := db.NewBatchWithSize(2048)
	writeBlocks(batch, blocks, receipts)
	require.NoError(t, batch.Write())

	for _, block := range blocks {
		require.True(t, rawdb.HasHeader(db, block.Hash(), block.NumberU64()))
		require.True(t, rawdb.HasBody(db, block.Hash(), block.NumberU64()))
		require.True(t, rawdb.HasReceipts(db, block.Hash(), block.NumberU64()))
	}
}

func TestDatabaseWriteSameBlockTwice(t *testing.T) {
	dataDir := t.TempDir()
	db, _ := newDatabasesFromDir(t, dataDir)
	blocks, _ := createBlocks(t, 2)
	block := blocks[1]

	// Write same block twice
	rawdb.WriteBlock(db, block)
	rawdb.WriteBlock(db, block)

	// Verify block data is still correct after duplicate writes
	actualBlock := rawdb.ReadBlock(db, block.Hash(), block.NumberU64())
	requireRLPEqual(t, block, actualBlock)
}

func TestDatabaseWriteDiffBlocksSameHeight(t *testing.T) {
	dataDir := t.TempDir()
	db, _ := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 2)
	block1 := blocks[1]
	receipt1 := receipts[1]

	// Manually create a second block with the same height but different content
	_, blocks2, receipts2, err := core.GenerateChainWithGenesis(&core.Genesis{
		Config: params.TestChainConfig,
		Alloc:  types.GenesisAlloc{addr1: {Balance: big.NewInt(params.Ether)}},
	}, dummy.NewFaker(), 1, 10, func(_ int, gen *core.BlockGen) {
		gen.OffsetTime(int64(5000))
		tx, _ := types.SignTx(types.NewTx(&types.DynamicFeeTx{
			ChainID:   params.TestChainConfig.ChainID,
			Nonce:     gen.TxNonce(addr1),
			To:        &addr2,
			Gas:       450000,
			GasTipCap: big.NewInt(5),
			GasFeeCap: big.NewInt(5),
		}), types.LatestSigner(params.TestChainConfig), key1)
		gen.AddTx(tx)
	})
	require.NoError(t, err)
	block2 := blocks2[0]
	receipt2 := receipts2[0]

	// Ensure both blocks have the same height but different hashes
	require.Equal(t, block1.NumberU64(), block2.NumberU64())
	require.NotEqual(t, block1.Hash(), block2.Hash())

	// Write two blocks with the same height
	writeBlocks(db, []*types.Block{block1, block2}, []types.Receipts{receipt1, receipt2})

	// Reading by the first block's hash does not return anything
	firstHeader := rawdb.ReadHeader(db, block1.Hash(), block1.NumberU64())
	require.Nil(t, firstHeader)
	firstBody := rawdb.ReadBody(db, block1.Hash(), block1.NumberU64())
	require.Nil(t, firstBody)
	firstReceipts := rawdb.ReadReceipts(db, block1.Hash(), block1.NumberU64(), block1.Time(), params.TestChainConfig)
	require.Nil(t, firstReceipts)

	// Verify Has also returns false for first block (hash mismatch)
	require.False(t, rawdb.HasHeader(db, block1.Hash(), block1.NumberU64()))
	require.False(t, rawdb.HasBody(db, block1.Hash(), block1.NumberU64()))
	require.False(t, rawdb.HasReceipts(db, block1.Hash(), block1.NumberU64()))

	// Reading by the second block's hash returns second block data
	secondBlock := rawdb.ReadBlock(db, block2.Hash(), block2.NumberU64())
	requireRLPEqual(t, block2, secondBlock)
	secondReceipts := rawdb.ReadReceipts(db, block2.Hash(), block2.NumberU64(), block2.Time(), params.TestChainConfig)
	requireRLPEqual(t, receipt2, secondReceipts)
}

func TestDatabaseReopen(t *testing.T) {
	dataDir := t.TempDir()
	db, _ := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 2)
	writeBlocks(db, blocks, receipts)
	b1 := blocks[1]
	r1 := receipts[1]

	// Close the db and verify we can no longer read block data
	require.NoError(t, db.Close())
	block := rawdb.ReadBlock(db, b1.Hash(), b1.NumberU64())
	require.Nil(t, block)
	recs := rawdb.ReadReceipts(db, b1.Hash(), b1.NumberU64(), b1.Time(), params.TestChainConfig)
	require.Nil(t, recs)
	_, err := db.headerDB.Get(b1.NumberU64())
	require.ErrorIs(t, err, database.ErrClosed)

	// Reopen the database and data can be read again
	db, _ = newDatabasesFromDir(t, dataDir)
	persistedBlock := rawdb.ReadBlock(db, b1.Hash(), b1.NumberU64())
	requireRLPEqual(t, b1, persistedBlock)
	persistedRecs := rawdb.ReadReceipts(db, b1.Hash(), b1.NumberU64(), b1.Time(), params.TestChainConfig)
	requireRLPEqual(t, r1, persistedRecs)
}

func TestDatabaseNewScenarios(t *testing.T) {
	blocks, _ := createBlocks(t, 10)

	tests := []struct {
		name          string
		stateSync     bool
		kvDBBlocks    []*types.Block
		dbMinHeight   uint64
		wantDBReady   bool
		wantMinHeight uint64
	}{
		{
			name:          "empty kvDB and state sync disabled",
			wantDBReady:   true,
			wantMinHeight: 1,
		},
		{
			name:          "empty kvDB and state sync enabled",
			stateSync:     true,
			wantDBReady:   false,
			wantMinHeight: 0,
		},
		{
			name:          "non genesis blocks to migrate",
			kvDBBlocks:    blocks[5:10],
			wantDBReady:   true,
			wantMinHeight: 5,
		},
		{
			name:          "blocks to migrate - including genesis",
			kvDBBlocks:    slices.Concat([]*types.Block{blocks[0]}, blocks[5:10]),
			wantDBReady:   true,
			wantMinHeight: 5,
		},
		{
			name:          "blocks to migrate and state sync enabled",
			stateSync:     true,
			kvDBBlocks:    blocks[5:10],
			wantDBReady:   true,
			wantMinHeight: 5,
		},
		{
			name:          "existing db created with min height",
			dbMinHeight:   2,
			kvDBBlocks:    blocks[5:8],
			wantDBReady:   true,
			wantMinHeight: 2,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			base, err := leveldb.New(dataDir, nil, logging.NoLog{}, prometheus.NewRegistry())
			require.NoError(t, err)
			kvDB := rawdb.NewDatabase(evmdb.WrapDatabase(base))

			// Create block databases with existing min height if needed
			if tc.dbMinHeight > 0 {
				db := Database{
					stateDB:  base,
					Database: kvDB,
					dbPath:   dataDir,
					config:   heightindexdb.DefaultConfig(),
					reg:      prometheus.NewRegistry(),
					logger:   logging.NoLog{},
				}
				require.NoError(t, db.InitBlockDBs(tc.dbMinHeight))
				require.NoError(t, db.headerDB.Close())
				require.NoError(t, db.bodyDB.Close())
				require.NoError(t, db.receiptsDB.Close())
				minHeight, ok, err := getDatabaseMinHeight(base)
				require.NoError(t, err)
				require.True(t, ok)
				require.Equal(t, tc.dbMinHeight, minHeight)
			}

			writeBlocks(kvDB, tc.kvDBBlocks, []types.Receipts{})
			db, _, err := New(
				base,
				kvDB,
				dataDir,
				tc.stateSync,
				heightindexdb.DefaultConfig(),
				logging.NoLog{},
				prometheus.NewRegistry(),
			)
			require.NoError(t, err)
			require.Equal(t, tc.wantDBReady, db.heightDBsReady)
			require.Equal(t, tc.wantMinHeight, db.minHeight)
		})
	}
}

func TestDatabaseGenesis(t *testing.T) {
	// Verifies that genesis blocks (block 0) only exist in kvDB and not
	// in the height-indexed databases.
	dataDir := t.TempDir()
	db, kvDB := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 1) // first block is genesis
	writeBlocks(db, blocks, receipts)

	// validate genesis block can be retrieved and its stored in kvDB
	hash := rawdb.ReadCanonicalHash(kvDB, 0)
	block := rawdb.ReadBlock(db, hash, 0)
	requireRLPEqual(t, blocks[0], block)
	_, err := db.headerDB.Get(0)
	require.ErrorIs(t, err, database.ErrNotFound)
	_, err = db.receiptsDB.Get(0)
	require.ErrorIs(t, err, database.ErrNotFound)
	require.Equal(t, uint64(1), db.minHeight)
}

func TestDatabaseInitMinHeight(t *testing.T) {
	// Verifies database min height is set on first init
	dataDir := t.TempDir()
	base, err := leveldb.New(dataDir, nil, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(t, err)
	kvDB := rawdb.NewDatabase(evmdb.WrapDatabase(base))

	// Initially not enabled
	enabled, err := IsEnabled(base)
	require.NoError(t, err)
	require.False(t, enabled)

	// Initialize with min height 3
	db, _, err := New(
		base,
		kvDB,
		dataDir,
		false,
		heightindexdb.DefaultConfig(),
		logging.NoLog{},
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)

	// Now enabled
	enabled, err = IsEnabled(base)
	require.NoError(t, err)
	require.True(t, enabled)

	// Call InitBlockDBs again should return error
	err = db.InitBlockDBs(5)
	require.ErrorIs(t, err, errAlreadyInitialized)
	require.Equal(t, uint64(1), db.minHeight)
}

func TestDatabaseMinHeightGating(t *testing.T) {
	// Verifies writes are gated by minHeight: below threshold go to kvDB,
	// at/above threshold go to height-index DBs.
	dataDir := t.TempDir()
	base, err := leveldb.New(dataDir, nil, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(t, err)
	kvDB := rawdb.NewDatabase(evmdb.WrapDatabase(base))

	db, _, err := New(
		base, kvDB, dataDir, true,
		heightindexdb.DefaultConfig(),
		logging.NoLog{},
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)
	require.NoError(t, db.InitBlockDBs(10))

	blocks, receipts := createBlocks(t, 11)

	// write block 9 (below minHeight) and block 10 (at minHeight)
	writeBlocks(db, blocks[9:11], receipts[9:11])

	// below threshold should not be in height DBs but in kvDB
	has, err := db.headerDB.Has(9)
	require.NoError(t, err)
	require.False(t, has)
	has, err = db.bodyDB.Has(9)
	require.NoError(t, err)
	require.False(t, has)
	has, err = db.receiptsDB.Has(9)
	require.NoError(t, err)
	require.False(t, has)
	require.True(t, rawdb.HasHeader(kvDB, blocks[9].Hash(), 9))
	require.True(t, rawdb.HasBody(kvDB, blocks[9].Hash(), 9))
	require.True(t, rawdb.HasReceipts(kvDB, blocks[9].Hash(), 9))

	// at/above threshold should be in height DBs
	_, err = db.bodyDB.Get(10)
	require.NoError(t, err)
	_, err = db.headerDB.Get(10)
	require.NoError(t, err)
	_, err = db.receiptsDB.Get(10)
	require.NoError(t, err)
	require.Nil(t, rawdb.ReadBlock(kvDB, blocks[10].Hash(), 10))
	require.False(t, rawdb.HasReceipts(kvDB, blocks[10].Hash(), 10))
}

func TestDatabaseHasReturnsFalseOnHashMismatch(t *testing.T) {
	dataDir := t.TempDir()
	db, _ := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 3)
	writeBlocks(db, blocks[1:3], receipts[1:3])

	// Query Has using height 2 with block 1's hash (mismatch)
	require.False(t, rawdb.HasHeader(db, blocks[1].Hash(), blocks[2].NumberU64()))
	require.False(t, rawdb.HasBody(db, blocks[1].Hash(), blocks[2].NumberU64()))
	require.False(t, rawdb.HasReceipts(db, blocks[1].Hash(), blocks[2].NumberU64()))
}

func TestDatabaseAlreadyInitializedError(t *testing.T) {
	dataDir := t.TempDir()
	db, _ := newDatabasesFromDir(t, dataDir)

	require.ErrorIs(t, db.InitBlockDBs(5), errAlreadyInitialized)
	require.Equal(t, uint64(1), db.minHeight)
}

func TestDatabaseGetNotFoundOnHashMismatch(t *testing.T) {
	dataDir := t.TempDir()
	db, _ := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 3)
	writeBlocks(db, blocks, receipts)

	headerKey := blockHeaderKey(1, blocks[0].Hash())
	_, err := db.Get(headerKey)
	require.ErrorIs(t, err, database.ErrNotFound)

	bodyKey := blockBodyKey(1, blocks[0].Hash())
	_, err = db.Get(bodyKey)
	require.ErrorIs(t, err, database.ErrNotFound)

	receiptsKey := receiptsKey(1, blocks[0].Hash())
	_, err = db.Get(receiptsKey)
	require.ErrorIs(t, err, database.ErrNotFound)
}
