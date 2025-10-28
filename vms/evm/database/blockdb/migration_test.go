// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"slices"
	"testing"
	"time"

	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/params"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/utils/logging"

	heightindexdb "github.com/ava-labs/avalanchego/x/blockdb"
	evmdb "github.com/ava-labs/coreth/plugin/evm/database"
)

func TestMigrationCompletion(t *testing.T) {
	tests := []struct {
		name          string
		want          bool
		dataToMigrate bool
		migrate       bool
	}{
		{
			name: "completed if no data to migrate",
			want: true,
		},
		{
			name:          "not completed if data to migrate",
			dataToMigrate: true,
			want:          false,
		},
		{
			name:          "complete after migration",
			dataToMigrate: true,
			migrate:       true,
			want:          true,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			dataDir := t.TempDir()
			base, err := leveldb.New(dataDir, nil, logging.NoLog{}, prometheus.NewRegistry())
			require.NoError(t, err)
			kvDB := rawdb.NewDatabase(evmdb.WrapDatabase(base))

			if test.dataToMigrate {
				blocks, receipts := createBlocks(t, 5)
				writeBlocks(kvDB, blocks, receipts)
			}

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
			if test.migrate {
				startMigration(t, db, true)
			}
			require.Equal(t, test.want, db.migrator.isCompleted())
		})
	}
}

func TestDatabaseMigrationInProcess(t *testing.T) {
	// Verifies that blocks are readable during migration for both migrated
	// and un-migrated blocks.
	// The test: generates 21 blocks, migrates 20, waits for 5 blocks to be migrated,
	// writes block 21 during migration, then verifies all blocks are readable.
	dataDir := t.TempDir()
	db, kvDB := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 21)

	// Add first 20 blocks to KVDB to migrate
	writeBlocks(kvDB, blocks[:20], receipts[:20])

	// Use blocking database to block after 5 blocks
	n := 0
	db.migrator.bodyDB = &blockingDatabase{
		HeightIndex: db.bodyDB,
		shouldBlock: func() bool {
			n++
			return n > 5
		},
	}
	startMigration(t, db, false)
	require.Eventually(t, func() bool {
		return db.migrator.processed.Load() >= 5
	}, 5*time.Second, 100*time.Millisecond)

	// Write block 21 to simulate new block being added during migration
	writeBlocks(db, blocks[20:21], receipts[20:21])

	// Verify all 21 blocks are readable via the db
	for i, block := range blocks {
		num := block.NumberU64()
		expReceipts := receipts[i]

		// we should be able to fetch block, receipts and logs
		actualBlock := rawdb.ReadBlock(db, block.Hash(), num)
		requireRLPEqual(t, block, actualBlock)
		actualReceipts := rawdb.ReadReceipts(db, block.Hash(), num, block.Time(), params.TestChainConfig)
		requireRLPEqual(t, expReceipts, actualReceipts)
		actualLogs := rawdb.ReadLogs(db, block.Hash(), num)
		requireRLPEqual(t, logsFromReceipts(expReceipts), actualLogs)

		// Header number should also be readable
		actualNum := rawdb.ReadHeaderNumber(db, block.Hash())
		require.NotNil(t, actualNum)
		require.Equal(t, num, *actualNum)
	}
}

func TestMigrationStart(t *testing.T) {
	testCases := []struct {
		name             string
		toMigrateHeights []uint64
		migratedHeights  []uint64
	}{
		{
			name:             "migrate blocks 0-4",
			toMigrateHeights: []uint64{0, 1, 2, 3, 4},
		},
		{
			name:             "migrate blocks 20-24",
			toMigrateHeights: []uint64{20, 21, 22, 23, 24},
		},
		{
			name:             "migrate non consecutive blocks",
			toMigrateHeights: []uint64{20, 21, 22, 29, 30, 40},
		},
		{
			name:             "migrated 0-5 and to migrate 6-10",
			toMigrateHeights: []uint64{6, 7, 8, 9, 10},
			migratedHeights:  []uint64{0, 1, 2, 3, 4, 5},
		},
		{
			name:            "all blocks migrated",
			migratedHeights: []uint64{0, 1, 2, 3, 4, 5},
		},
		{
			name: "no blocks to migrate or migrated",
		},
		{
			name:             "non consecutive blocks migrated and blocks to migrate",
			toMigrateHeights: []uint64{2, 3, 7, 8, 10},
			migratedHeights:  []uint64{0, 1, 4, 5, 9},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			db, kvDB := newDatabasesFromDir(t, dataDir)
			allHeights := slices.Concat(tc.toMigrateHeights, tc.migratedHeights)
			var maxHeight uint64
			if len(allHeights) > 0 {
				maxHeight = slices.Max(allHeights)
			}
			blocks, receipts := createBlocks(t, int(maxHeight)+1)

			// Set initial db state
			for _, height := range tc.toMigrateHeights {
				writeBlocks(kvDB, []*types.Block{blocks[height]}, []types.Receipts{receipts[height]})
			}
			for _, height := range tc.migratedHeights {
				writeBlocks(db, []*types.Block{blocks[height]}, []types.Receipts{receipts[height]})
			}

			// Verify all blocks and receipts are accessible after migration
			startMigration(t, db, true)
			for _, height := range allHeights {
				expBlock := blocks[height]
				expReceipts := receipts[height]
				block := rawdb.ReadBlock(db, expBlock.Hash(), height)
				requireRLPEqual(t, expBlock, block)
				receipts := rawdb.ReadReceipts(db, expBlock.Hash(), height, expBlock.Time(), params.TestChainConfig)
				requireRLPEqual(t, expReceipts, receipts)
				logs := rawdb.ReadLogs(db, expBlock.Hash(), height)
				requireRLPEqual(t, logsFromReceipts(expReceipts), logs)

				// verify kvDB no longer has any blocks or receipts (except for genesis)
				if height != 0 {
					require.False(t, rawdb.HasHeader(kvDB, expBlock.Hash(), height))
					require.False(t, rawdb.HasBody(kvDB, expBlock.Hash(), height))
					require.False(t, rawdb.HasReceipts(kvDB, expBlock.Hash(), height))
				}
			}
		})
	}
}

func TestMigrationStopAndResume(t *testing.T) {
	// Verifies that migration can be stopped mid-run and resumed from where it left off.
	dataDir := t.TempDir()
	db, kvDB := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 10)
	writeBlocks(kvDB, blocks, receipts)

	// Block migration process after 3 blocks
	n := 0
	db.migrator.bodyDB = &blockingDatabase{
		HeightIndex: db.bodyDB,
		shouldBlock: func() bool {
			n++
			return n > 3
		},
	}
	startMigration(t, db, false)
	require.Eventually(t, func() bool {
		return db.migrator.processed.Load() >= 3
	}, 5*time.Second, 100*time.Millisecond)
	require.False(t, db.migrator.isCompleted())

	// Verify blocks 1-3 were migrated
	for i := 1; i <= 3; i++ {
		has, err := db.bodyDB.Has(uint64(i))
		require.NoError(t, err)
		require.True(t, has)
	}

	// Verify blocks 4-9 is not migrated
	for i := 4; i <= 9; i++ {
		require.True(t, rawdb.HasHeader(kvDB, blocks[i].Hash(), uint64(i)))
		has, err := db.bodyDB.Has(uint64(i))
		require.NoError(t, err)
		require.False(t, has)
	}

	// Cut off migration and start again
	require.NoError(t, db.Database.Close())
	db, kvDB = newDatabasesFromDir(t, dataDir)
	require.False(t, db.migrator.isCompleted())
	startMigration(t, db, true)

	// Verify that all blocks are accessible after migration
	for i, block := range blocks {
		actualBlock := rawdb.ReadBlock(db, block.Hash(), block.NumberU64())
		requireRLPEqual(t, block, actualBlock)
		actualReceipts := rawdb.ReadReceipts(db, block.Hash(), block.NumberU64(), block.Time(), params.TestChainConfig)
		requireRLPEqual(t, receipts[i], actualReceipts)
	}
}

func TestMigrationSkipsGenesis(t *testing.T) {
	dataDir := t.TempDir()
	base, err := leveldb.New(dataDir, nil, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(t, err)
	kvDB := rawdb.NewDatabase(evmdb.WrapDatabase(base))
	blocks, receipts := createBlocks(t, 10)
	writeBlocks(kvDB, blocks[0:1], receipts[0:1])
	writeBlocks(kvDB, blocks[5:10], receipts[5:10])

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
	require.True(t, db.heightDBsReady)
	require.Equal(t, uint64(5), db.minHeight)

	// migrate and and verify genesis block is not migrated
	startMigration(t, db, true)
	genesisHash := rawdb.ReadCanonicalHash(kvDB, 0)
	require.True(t, rawdb.HasHeader(kvDB, genesisHash, 0))
	has, err := db.bodyDB.Has(0)
	require.NoError(t, err)
	require.False(t, has)
}

func TestMigrationWithoutReceipts(t *testing.T) {
	dataDir := t.TempDir()
	base, err := leveldb.New(dataDir, nil, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(t, err)
	kvDB := rawdb.NewDatabase(evmdb.WrapDatabase(base))
	blocks, _ := createBlocks(t, 5)

	// Write blocks without receipts to kvDB
	for _, block := range blocks {
		rawdb.WriteBlock(kvDB, block)
		rawdb.WriteCanonicalHash(kvDB, block.Hash(), block.NumberU64())
	}

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
	startMigration(t, db, true)
	require.True(t, db.migrator.isCompleted())

	// Verify all blocks are accessible and receipts are nil
	for _, block := range blocks {
		actualBlock := rawdb.ReadBlock(db, block.Hash(), block.NumberU64())
		requireRLPEqual(t, block, actualBlock)
		recs := rawdb.ReadReceipts(db, block.Hash(), block.NumberU64(), block.Time(), params.TestChainConfig)
		require.Nil(t, recs)
	}
}
