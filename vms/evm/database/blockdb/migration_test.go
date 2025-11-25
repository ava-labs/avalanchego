// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"slices"
	"testing"

	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/params"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/utils/logging"

	evmdb "github.com/ava-labs/avalanchego/vms/evm/database"
	heightindexdb "github.com/ava-labs/avalanchego/x/blockdb"
)

func TestMigrationCompletion(t *testing.T) {
	tests := []struct {
		name          string
		want          bool
		dataToMigrate bool
		migrate       bool
	}{
		{
			name: "completed when no data to migrate",
			want: true,
		},
		{
			name:          "not completed if data to migrate",
			dataToMigrate: true,
		},
		{
			name:          "completed after migration",
			dataToMigrate: true,
			migrate:       true,
			want:          true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			base, err := leveldb.New(dataDir, nil, logging.NoLog{}, prometheus.NewRegistry())
			require.NoError(t, err)
			evmDB := rawdb.NewDatabase(evmdb.New(base))

			if tc.dataToMigrate {
				blocks, receipts := createBlocks(t, 5)
				writeBlocks(evmDB, blocks, receipts)
			}

			db, _, err := New(
				base,
				evmDB,
				dataDir,
				false,
				heightindexdb.DefaultConfig(),
				logging.NoLog{},
				prometheus.NewRegistry(),
			)
			require.NoError(t, err)
			if tc.migrate {
				startMigration(t, db, true)
			}
			require.Equal(t, tc.want, db.migrator.isCompleted())
		})
	}
}

func TestMigrationInProcess(t *testing.T) {
	// Verifies blocks are readable during migration for both migrated
	// and un-migrated blocks.
	// The test generates 21 blocks, migrates 20 but pauses after 5,
	// writes block 21, and verifies migrated and un-migrated blocks are readable.
	dataDir := t.TempDir()
	db, evmDB := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 21)

	// add blocks 0-19 to KVDB to migrate
	writeBlocks(evmDB, blocks[0:20], receipts[0:20])

	// migrate blocks 1-6
	startPartialMigration(t, db, 6)

	// write block 20 to simulate new block being added during migration
	writeBlocks(db, blocks[20:21], receipts[20:21])

	// verify all 21 blocks are readable via the db
	for i, block := range blocks {
		num := block.NumberU64()
		expReceipts := receipts[i]

		// We should be able to fetch block, receipts and logs.
		actualBlock := rawdb.ReadBlock(db, block.Hash(), num)
		requireRLPEqual(t, block, actualBlock)
		actualReceipts := rawdb.ReadReceipts(db, block.Hash(), num, block.Time(), params.TestChainConfig)
		requireRLPEqual(t, expReceipts, actualReceipts)
		actualLogs := rawdb.ReadLogs(db, block.Hash(), num)
		requireRLPEqual(t, logsFromReceipts(expReceipts), actualLogs)

		// header number should also be readable
		actualNum := rawdb.ReadHeaderNumber(db, block.Hash())
		require.NotNil(t, actualNum)
		require.Equal(t, num, *actualNum)

		// Block 1-6 and 20 should be migrated, others should not.
		has, err := db.headerDB.Has(num)
		require.NoError(t, err)
		migrated := (num >= 1 && num <= 6) || num == 20
		require.Equal(t, migrated, has)
	}
}

func TestMigrationStart(t *testing.T) {
	tests := []struct {
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

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			db, evmDB := newDatabasesFromDir(t, dataDir)
			allHeights := slices.Concat(tc.toMigrateHeights, tc.migratedHeights)
			var maxHeight uint64
			if len(allHeights) > 0 {
				maxHeight = slices.Max(allHeights)
			}
			blocks, receipts := createBlocks(t, int(maxHeight)+1)

			// set initial db state
			for _, height := range tc.toMigrateHeights {
				writeBlocks(evmDB, []*types.Block{blocks[height]}, []types.Receipts{receipts[height]})
			}
			for _, height := range tc.migratedHeights {
				writeBlocks(db, []*types.Block{blocks[height]}, []types.Receipts{receipts[height]})
			}

			// Verify all blocks and receipts are accessible after migration.
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

				// Verify evmDB no longer has any blocks or receipts (except for genesis).
				hasData := height == 0
				require.Equal(t, hasData, rawdb.HasHeader(evmDB, expBlock.Hash(), height))
				require.Equal(t, hasData, rawdb.HasBody(evmDB, expBlock.Hash(), height))
				require.Equal(t, hasData, rawdb.HasReceipts(evmDB, expBlock.Hash(), height))
			}
		})
	}
}

func TestMigrationResume(t *testing.T) {
	// Verifies migration can be stopped mid-run and resumed.
	dataDir := t.TempDir()
	db, evmDB := newDatabasesFromDir(t, dataDir)
	blocks, receipts := createBlocks(t, 10)
	writeBlocks(evmDB, blocks, receipts)

	// block migration after 3 blocks
	startPartialMigration(t, db, 3)
	require.False(t, db.migrator.isCompleted())

	for i := 0; i < 10; i++ {
		migrated := i >= 1 && i <= 3 // blocks 1-3 are migrated
		has, err := db.bodyDB.Has(uint64(i))
		require.NoError(t, err)
		require.Equal(t, migrated, has)
	}

	// stop migration and start again
	require.NoError(t, db.Database.Close())
	db, _ = newDatabasesFromDir(t, dataDir)
	require.False(t, db.migrator.isCompleted())
	startMigration(t, db, true)

	// verify all blocks are accessible after migration
	for i, block := range blocks {
		num := block.NumberU64()
		hash := block.Hash()
		actualBlock := rawdb.ReadBlock(db, hash, num)
		requireRLPEqual(t, block, actualBlock)
		actualReceipts := rawdb.ReadReceipts(db, hash, num, block.Time(), params.TestChainConfig)
		requireRLPEqual(t, receipts[i], actualReceipts)
	}
}

func TestMigrationSkipsGenesis(t *testing.T) {
	dataDir := t.TempDir()
	base, err := leveldb.New(dataDir, nil, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(t, err)
	evmDB := rawdb.NewDatabase(evmdb.New(base))
	blocks, receipts := createBlocks(t, 10)
	writeBlocks(evmDB, blocks[0:1], receipts[0:1])
	writeBlocks(evmDB, blocks[5:10], receipts[5:10])

	db, _, err := New(
		base,
		evmDB,
		dataDir,
		false,
		heightindexdb.DefaultConfig(),
		logging.NoLog{},
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)
	require.True(t, db.heightDBsReady)
	require.Equal(t, uint64(5), db.minHeight)

	// migrate and verify genesis block is not migrated
	startMigration(t, db, true)
	require.True(t, db.migrator.isCompleted())
	genHash := rawdb.ReadCanonicalHash(evmDB, 0)
	require.True(t, rawdb.HasHeader(evmDB, genHash, 0))
	has, err := db.bodyDB.Has(0)
	require.NoError(t, err)
	require.False(t, has)
}

func TestMigrationWithoutReceipts(t *testing.T) {
	dataDir := t.TempDir()
	base, err := leveldb.New(dataDir, nil, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(t, err)
	evmDB := rawdb.NewDatabase(evmdb.New(base))
	blocks, _ := createBlocks(t, 5)

	// write blocks without receipts to evmDB
	for _, block := range blocks {
		rawdb.WriteBlock(evmDB, block)
		rawdb.WriteCanonicalHash(evmDB, block.Hash(), block.NumberU64())
	}

	db, initialized, err := New(
		base,
		evmDB,
		dataDir,
		false,
		heightindexdb.DefaultConfig(),
		logging.NoLog{},
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)
	require.True(t, initialized)
	startMigration(t, db, true)
	require.True(t, db.migrator.isCompleted())

	// verify all blocks are accessible and receipts are nil
	for _, block := range blocks {
		actualBlock := rawdb.ReadBlock(db, block.Hash(), block.NumberU64())
		requireRLPEqual(t, block, actualBlock)
		recs := rawdb.ReadReceipts(db, block.Hash(), block.NumberU64(), block.Time(), params.TestChainConfig)
		require.Nil(t, recs)
	}
}
