// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/vms/evm/database"
	"github.com/ava-labs/avalanchego/vms/evm/database/blockdb"

	avalanchedatabase "github.com/ava-labs/avalanchego/database"
	heightindexdb "github.com/ava-labs/avalanchego/x/blockdb"
)

// initializeDBs initializes the databases used by the VM.
// coreth always uses the avalanchego provided database.
func (vm *VM) initializeDBs(db avalanchedatabase.Database) error {
	vm.versiondb = versiondb.New(db)
	vm.acceptedBlockDB = prefixdb.New(acceptedPrefix, vm.versiondb)
	vm.metadataDB = prefixdb.New(metadataPrefix, vm.versiondb)
	// Note warpDB is not part of versiondb because it is not necessary
	// that warp signatures are committed to the database atomically with
	// the last accepted block.
	vm.warpDB = prefixdb.New(warpPrefix, db)

	// newChainDB must be created after acceptedBlockDB because it uses it to
	// determine if state sync is enabled.
	chaindb, err := vm.newChainDB(db)
	if err != nil {
		return err
	}
	vm.chaindb = chaindb
	return nil
}

func (vm *VM) inspectDatabases() error {
	start := time.Now()
	log.Info("Starting database inspection")
	if err := rawdb.InspectDatabase(vm.chaindb, nil, nil, rawdb.WithSkipFreezers()); err != nil {
		return err
	}
	if err := inspectDB(vm.acceptedBlockDB, "acceptedBlockDB"); err != nil {
		return err
	}
	if err := inspectDB(vm.metadataDB, "metadataDB"); err != nil {
		return err
	}
	if err := inspectDB(vm.warpDB, "warpDB"); err != nil {
		return err
	}
	log.Info("Completed database inspection", "elapsed", time.Since(start))
	return nil
}

func inspectDB(db avalanchedatabase.Database, label string) error {
	it := db.NewIterator()
	defer it.Release()

	var (
		count  int64
		start  = time.Now()
		logged = time.Now()

		// Totals
		total common.StorageSize
	)
	// Inspect key-value database first.
	for it.Next() {
		var (
			key  = it.Key()
			size = common.StorageSize(len(key) + len(it.Value()))
		)
		total += size
		count++
		if count%1000 == 0 && time.Since(logged) > 8*time.Second {
			log.Info("Inspecting database", "label", label, "count", count, "elapsed", common.PrettyDuration(time.Since(start)))
			logged = time.Now()
		}
	}
	// Display the database statistic.
	log.Info("Database statistics", "label", label, "total", total.String(), "count", count)
	return nil
}

// newChainDB creates a new chain database.
// If block database is enabled, it will create a blockdb.Database that
// stores blocks data in separate databases.
// If block database is not enabled but had been previously enabled, it will return an error.
func (vm *VM) newChainDB(db avalanchedatabase.Database) (ethdb.Database, error) {
	// Use NewNested rather than New so that the structure of the database
	// remains the same regardless of the provided baseDB type.
	chainDB := rawdb.NewDatabase(database.New(prefixdb.NewNested(ethDBPrefix, db)))

	// Error if block database has been created and then disabled
	metaDB := prefixdb.New(blockDBPrefix, db)
	enabled, err := blockdb.IsEnabled(metaDB)
	if err != nil {
		return nil, err
	}
	if !vm.config.BlockDatabaseEnabled {
		if enabled {
			return nil, errors.New("block database should not be disabled after it has been enabled")
		}
		return chainDB, nil
	}

	version := strconv.FormatUint(heightindexdb.IndexFileVersion, 10)
	path := filepath.Join(vm.ctx.ChainDataDir, "blockdb", version)
	_, lastAcceptedHeight, err := vm.ReadLastAccepted()
	if err != nil {
		return nil, err
	}
	stateSyncEnabled := vm.stateSyncEnabled(lastAcceptedHeight)
	cfg := heightindexdb.DefaultConfig().WithSyncToDisk(false)
	blockDB, initialized, err := blockdb.New(
		metaDB,
		chainDB,
		path,
		stateSyncEnabled,
		cfg,
		vm.ctx.Log,
		vm.sdkMetrics,
	)
	if err != nil {
		return nil, err
	}
	if initialized && !vm.config.SkipBlockDatabaseAutoMigrate {
		if err := blockDB.StartMigration(context.Background()); err != nil {
			return nil, err
		}
	}
	return blockDB, nil
}
