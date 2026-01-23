// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"slices"
	"testing"

	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/params"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/utils/logging"

	evmdb "github.com/ava-labs/avalanchego/vms/evm/database"
	heightindexdb "github.com/ava-labs/avalanchego/x/blockdb"
)

func TestDatabaseWriteAndReadBlock(t *testing.T) {
	dataDir := t.TempDir()
	db, _ := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 10)
	writeBlocks(db, blocks, receipts)

	for _, block := range blocks {
		actualBlock := rawdb.ReadBlock(db, block.Hash(), block.NumberU64())
		requireRLPEqual(t, block, actualBlock)
	}
}

func TestDatabaseWriteAndReadReceipts(t *testing.T) {
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
		recs := receipts[i]
		requireRLPEqual(t, logsFromReceipts(recs), actualLogs)
	}
}

func TestDatabaseDeleteBlocksNoOp(t *testing.T) {
	// Verifies that block header, body and receipts cannot be deleted (no-op),
	// but hash to height mapping should be deleted.
	tests := []struct {
		name      string
		useBatch  bool
		batchSize int
	}{
		{name: "delete block data is a no-op"},
		{name: "batch delete", useBatch: true},
		{name: "batch delete with size", useBatch: true, batchSize: 1024},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			db, _ := newDatabasesFromDir(t, dataDir)
			allBlocks, allReceipts := createBlocks(t, 4)
			blocks := allBlocks[1:] // skip genesis block
			receipts := allReceipts[1:]
			writeBlocks(db, blocks, receipts)

			// perform delete operations on all blocks
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

			for i, block := range blocks {
				actualBlock := rawdb.ReadBlock(db, block.Hash(), block.NumberU64())
				requireRLPEqual(t, block, actualBlock)
				require.True(t, rawdb.HasReceipts(db, block.Hash(), block.NumberU64()))
				expReceipts := receipts[i]
				logs := rawdb.ReadLogs(db, block.Hash(), block.NumberU64())
				requireRLPEqual(t, logsFromReceipts(expReceipts), logs)

				// hash -> number mapping should be deleted
				num := rawdb.ReadHeaderNumber(db, block.Hash())
				require.Nil(t, num)
			}
		})
	}
}

func TestDatabaseWriteToHeightIndexedDB(t *testing.T) {
	dataDir := t.TempDir()
	db, evmDB := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 2)
	writeBlocks(db, blocks, receipts)
	block := blocks[1]

	// verify no block data in evmDB
	require.False(t, rawdb.HasHeader(evmDB, block.Hash(), block.NumberU64()))
	require.False(t, rawdb.HasBody(evmDB, block.Hash(), block.NumberU64()))
	require.False(t, rawdb.HasReceipts(evmDB, block.Hash(), block.NumberU64()))

	// verify block data in height-indexed databases
	ok, err := db.headerDB.Has(block.NumberU64())
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = db.bodyDB.Has(block.NumberU64())
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = db.receiptsDB.Has(block.NumberU64())
	require.NoError(t, err)
	require.True(t, ok)
}

func TestDatabaseNewBatch(t *testing.T) {
	dataDir := t.TempDir()
	db, _ := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 2)
	block := blocks[1]
	batch := db.NewBatch()
	writeBlocks(batch, blocks, receipts)

	// after adding blocks to batch, blocks and receipts should be available immediately
	require.True(t, rawdb.HasBody(db, block.Hash(), block.NumberU64()))
	require.True(t, rawdb.HasHeader(db, block.Hash(), block.NumberU64()))
	require.True(t, rawdb.HasReceipts(db, block.Hash(), block.NumberU64()))

	// header number should not be available until batch is written
	require.Nil(t, rawdb.ReadHeaderNumber(db, block.Hash()))
	require.NoError(t, batch.Write())
	num := rawdb.ReadHeaderNumber(db, block.Hash())
	require.Equal(t, block.NumberU64(), *num)
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

	// write same block twice
	rawdb.WriteBlock(db, block)
	rawdb.WriteBlock(db, block)

	// we should be able to read the block after duplicate writes
	actualBlock := rawdb.ReadBlock(db, block.Hash(), block.NumberU64())
	requireRLPEqual(t, block, actualBlock)
}

func TestDatabaseWriteDifferentBlocksAtSameHeight(t *testing.T) {
	dataDir := t.TempDir()
	db, _ := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 2)
	b1 := blocks[1]
	r1 := receipts[1]

	// create a second block with the same height but different tx
	to := addrFromTest(t, "different-to")
	blocks2, receipts2 := createBlocksToAddr(t, 2, to)
	b2 := blocks2[1]
	r2 := receipts2[1]

	// ensure both blocks have the same height but different hashes
	require.Equal(t, b1.NumberU64(), b2.NumberU64())
	require.NotEqual(t, b1.Hash(), b2.Hash())

	writeBlocks(db, []*types.Block{b1, b2}, []types.Receipts{r1, r2})

	// reading by the first block's hash should not return anything
	require.Nil(t, rawdb.ReadHeader(db, b1.Hash(), b1.NumberU64()))
	require.Nil(t, rawdb.ReadBody(db, b1.Hash(), b1.NumberU64()))
	require.Nil(t, rawdb.ReadReceipts(db, b1.Hash(), b1.NumberU64(), b1.Time(), params.TestChainConfig))

	// reading by the second block's hash returns second block data
	requireRLPEqual(t, b2, rawdb.ReadBlock(db, b2.Hash(), b2.NumberU64()))
	actualReceipts := rawdb.ReadReceipts(db, b2.Hash(), b2.NumberU64(), b2.Time(), params.TestChainConfig)
	requireRLPEqual(t, r2, actualReceipts)
}

func TestDatabaseReopen(t *testing.T) {
	dataDir := t.TempDir()
	db, _ := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 2)
	writeBlocks(db, blocks, receipts)
	b1 := blocks[1]
	r1 := receipts[1]

	// close db and verify we can no longer read block data
	require.NoError(t, db.Close())
	block := rawdb.ReadBlock(db, b1.Hash(), b1.NumberU64())
	require.Nil(t, block)
	recs := rawdb.ReadReceipts(db, b1.Hash(), b1.NumberU64(), b1.Time(), params.TestChainConfig)
	require.Nil(t, recs)
	_, err := db.headerDB.Get(b1.NumberU64())
	require.ErrorIs(t, err, database.ErrClosed)

	// reopen the database and data can be read again
	db, _ = newDatabasesFromDir(t, dataDir)
	block = rawdb.ReadBlock(db, b1.Hash(), b1.NumberU64())
	requireRLPEqual(t, b1, block)
	actualReceipts := rawdb.ReadReceipts(db, b1.Hash(), b1.NumberU64(), b1.Time(), params.TestChainConfig)
	requireRLPEqual(t, r1, actualReceipts)
}

func TestDatabaseInitialization(t *testing.T) {
	blocks, _ := createBlocks(t, 10)

	tests := []struct {
		name          string
		deferInit     bool
		evmDBBlocks   []*types.Block
		dbMinHeight   uint64
		wantDBReady   bool
		wantMinHeight uint64
	}{
		{
			name:          "empty evmDB and no deferred init",
			wantDBReady:   true,
			wantMinHeight: 1,
		},
		{
			name:        "empty evmDB and deferred init",
			deferInit:   true,
			wantDBReady: false, // db should not be ready due to deferred init
		},
		{
			name:          "existing db created with min height",
			evmDBBlocks:   blocks[5:8],
			dbMinHeight:   2,
			wantDBReady:   true,
			wantMinHeight: 2,
		},
		{
			name:          "non genesis blocks to migrate",
			evmDBBlocks:   blocks[5:10],
			wantDBReady:   true,
			wantMinHeight: 5,
		},
		{
			name:          "blocks to migrate - including genesis",
			evmDBBlocks:   slices.Concat([]*types.Block{blocks[0]}, blocks[5:10]),
			wantDBReady:   true,
			wantMinHeight: 5,
		},
		{
			name:          "blocks to migrate and deferred init",
			deferInit:     true,
			evmDBBlocks:   blocks[5:10],
			wantDBReady:   true,
			wantMinHeight: 5,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			base, err := leveldb.New(dataDir, nil, logging.NoLog{}, prometheus.NewRegistry())
			require.NoError(t, err)
			evmDB := rawdb.NewDatabase(evmdb.New(base))

			// create block databases with existing min height if needed
			if tc.dbMinHeight > 0 {
				db := Database{
					metaDB:   base,
					Database: evmDB,
					dbPath:   dataDir,
					config:   heightindexdb.DefaultConfig(),
					reg:      prometheus.NewRegistry(),
					logger:   logging.NoLog{},
				}
				require.NoError(t, db.InitBlockDBs(tc.dbMinHeight))
				require.NoError(t, db.headerDB.Close())
				require.NoError(t, db.bodyDB.Close())
				require.NoError(t, db.receiptsDB.Close())
				minHeight, ok, err := databaseMinHeight(base)
				require.NoError(t, err)
				require.True(t, ok)
				require.Equal(t, tc.dbMinHeight, minHeight)
			}

			writeBlocks(evmDB, tc.evmDBBlocks, []types.Receipts{})
			db, _, err := New(
				base,
				evmDB,
				dataDir,
				tc.deferInit,
				heightindexdb.DefaultConfig(),
				logging.NoLog{},
				prometheus.NewRegistry(),
			)
			require.NoError(t, err)
			require.Equal(t, tc.wantDBReady, db.heightDBsReady, "database ready mismatch")
			require.Equal(t, tc.wantMinHeight, db.minHeight, "database min height mismatch")
		})
	}
}

func TestDatabaseGenesisBlockHandling(t *testing.T) {
	// Verifies that genesis blocks (block 0) only exist in evmDB and not
	// in the height-indexed databases.
	dataDir := t.TempDir()
	db, evmDB := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 1) // first block is genesis
	writeBlocks(db, blocks, receipts)

	// Validate genesis block can be retrieved and is stored in evmDB.
	hash := rawdb.ReadCanonicalHash(evmDB, 0)
	block := rawdb.ReadBlock(db, hash, 0)
	requireRLPEqual(t, blocks[0], block)
	_, err := db.headerDB.Get(0)
	require.ErrorIs(t, err, database.ErrNotFound)
	_, err = db.receiptsDB.Get(0)
	require.ErrorIs(t, err, database.ErrNotFound)
	require.Equal(t, uint64(1), db.minHeight)
}

func TestDatabaseInitBlockDBs(t *testing.T) {
	dataDir := t.TempDir()
	base, err := leveldb.New(dataDir, nil, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(t, err)
	evmDB := rawdb.NewDatabase(evmdb.New(base))

	db, initialized, err := New(
		base,
		evmDB,
		dataDir,
		true,
		heightindexdb.DefaultConfig(),
		logging.NoLog{},
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)
	require.False(t, initialized)

	require.NoError(t, db.InitBlockDBs(10))
	require.Equal(t, uint64(10), db.minHeight)
}

func TestDatabaseMinHeightWrites(t *testing.T) {
	// Verifies writes are gated by minHeight: below threshold go to evmDB,
	// at/above threshold go to height-index DBs.
	dataDir := t.TempDir()
	base, err := leveldb.New(dataDir, nil, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(t, err)
	evmDB := rawdb.NewDatabase(evmdb.New(base))

	db, _, err := New(
		base,
		evmDB,
		dataDir,
		true,
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
	require.True(t, rawdb.HasHeader(evmDB, blocks[9].Hash(), 9))
	require.True(t, rawdb.HasBody(evmDB, blocks[9].Hash(), 9))
	require.True(t, rawdb.HasReceipts(evmDB, blocks[9].Hash(), 9))

	// at/above threshold should be in height DBs
	_, err = db.bodyDB.Get(10)
	require.NoError(t, err)
	_, err = db.headerDB.Get(10)
	require.NoError(t, err)
	_, err = db.receiptsDB.Get(10)
	require.NoError(t, err)
	require.Nil(t, rawdb.ReadBlock(evmDB, blocks[10].Hash(), 10))
	require.False(t, rawdb.HasReceipts(evmDB, blocks[10].Hash(), 10))
}

func TestDatabaseMinHeightReturnsErrorOnInvalidEncoding(t *testing.T) {
	dataDir := t.TempDir()
	db, err := leveldb.New(dataDir, nil, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(t, err)

	// write an incorrectly sized value for blockDBMinHeightKey
	require.NoError(t, db.Put(blockDBMinHeightKey, []byte{0x01}))

	_, ok, err := databaseMinHeight(db)
	require.ErrorIs(t, err, errInvalidEncodedLength)
	require.False(t, ok)
}

func TestDatabaseHasReturnsFalseOnHashMismatch(t *testing.T) {
	dataDir := t.TempDir()
	db, _ := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 3)
	writeBlocks(db, blocks[1:3], receipts[1:3])

	// fetch block 2 with block 1's hash
	require.False(t, rawdb.HasHeader(db, blocks[1].Hash(), blocks[2].NumberU64()))
	require.False(t, rawdb.HasBody(db, blocks[1].Hash(), blocks[2].NumberU64()))
	require.False(t, rawdb.HasReceipts(db, blocks[1].Hash(), blocks[2].NumberU64()))
}

func TestDatabaseAlreadyInitializedError(t *testing.T) {
	dataDir := t.TempDir()
	db, _ := newDatabasesFromDir(t, dataDir)

	err := db.InitBlockDBs(5)
	require.ErrorIs(t, err, errAlreadyInitialized)
	require.Equal(t, uint64(1), db.minHeight)
}

func TestDatabaseGetNotFoundOnHashMismatch(t *testing.T) {
	dataDir := t.TempDir()
	db, _ := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 3)
	writeBlocks(db, blocks, receipts)

	// get block 1 with block 0's hash
	_, err := db.Get(blockHeaderKey(1, blocks[0].Hash()))
	require.ErrorIs(t, err, database.ErrNotFound)
	_, err = db.Get(blockBodyKey(1, blocks[0].Hash()))
	require.ErrorIs(t, err, database.ErrNotFound)
	_, err = db.Get(receiptsKey(1, blocks[0].Hash()))
	require.ErrorIs(t, err, database.ErrNotFound)
}

func TestIsEnabled(t *testing.T) {
	// Verifies database min height is set on first init.
	dataDir := t.TempDir()
	base, err := leveldb.New(dataDir, nil, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(t, err)
	evmDB := rawdb.NewDatabase(evmdb.New(base))

	// initially not enabled
	enabled, err := IsEnabled(base)
	require.NoError(t, err)
	require.False(t, enabled)

	// create db but don't initialize
	db, initialized, err := New(
		base,
		evmDB,
		dataDir,
		true,
		heightindexdb.DefaultConfig(),
		logging.NoLog{},
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)
	require.False(t, initialized)

	// not enabled since InitBlockDBs was not called
	enabled, err = IsEnabled(base)
	require.NoError(t, err)
	require.False(t, enabled)

	// now enabled
	require.NoError(t, db.InitBlockDBs(10))
	enabled, err = IsEnabled(base)
	require.NoError(t, err)
	require.True(t, enabled)
}
