// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/database/factory"
	"github.com/ava-labs/avalanchego/database/meterdb"
	"github.com/ava-labs/avalanchego/database/pebbledb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/config"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/database"

	avalanchedatabase "github.com/ava-labs/avalanchego/database"
)

const (
	dbMetricsPrefix = "db"
	meterDBGatherer = "meterdb"
)

type DatabaseConfig struct {
	// If true, all writes are to memory and are discarded at shutdown.
	ReadOnly bool `json:"readOnly"`

	// Path to database
	Path string `json:"path"`

	// Name of the database type to use
	Name string `json:"name"`

	// Config bytes (JSON) for the database
	// See relevant (pebbledb, leveldb) config options
	Config []byte `json:"-"`
}

// initializeDBs initializes the databases used by the VM.
// If [useStandaloneDB] is true, the chain will use a standalone database for its state.
// Otherwise, the chain will use the provided [avaDB] for its state.
func (vm *VM) initializeDBs(avaDB avalanchedatabase.Database) error {
	db := avaDB
	// skip standalone database initialization if we are running in unit tests
	if vm.ctx.NetworkID != constants.UnitTestID {
		// first initialize the accepted block database to check if we need to use a standalone database
		acceptedDB := prefixdb.New(acceptedPrefix, avaDB)
		useStandAloneDB, err := vm.useStandaloneDatabase(acceptedDB)
		if err != nil {
			return err
		}
		if useStandAloneDB {
			// If we are using a standalone database, we need to create a new database
			// for the chain state.
			dbConfig, err := getDatabaseConfig(vm.config, vm.ctx.ChainDataDir)
			if err != nil {
				return err
			}
			log.Info("Using standalone database for the chain state", "DatabaseConfig", dbConfig)
			db, err = newStandaloneDatabase(dbConfig, vm.ctx.Metrics, vm.ctx.Log)
			if err != nil {
				return err
			}
			vm.usingStandaloneDB = true
		}
	}
	// Use NewNested rather than New so that the structure of the database
	// remains the same regardless of the provided baseDB type.
	vm.chaindb = rawdb.NewDatabase(database.New(prefixdb.NewNested(ethDBPrefix, db)))
	vm.versiondb = versiondb.New(db)
	vm.acceptedBlockDB = prefixdb.New(acceptedPrefix, vm.versiondb)
	vm.metadataDB = prefixdb.New(metadataPrefix, vm.versiondb)
	vm.db = db
	// Note warpDB and validatorsDB are not part of versiondb because it is not necessary
	// that they are committed to the database atomically with
	// the last accepted block.
	// [warpDB] is used to store warp message signatures
	// set to a prefixDB with the prefix [warpPrefix]
	vm.warpDB = prefixdb.New(warpPrefix, db)
	// [validatorsDB] is used to store the current validator set and uptimes
	// set to a prefixDB with the prefix [validatorsDBPrefix]
	vm.validatorsDB = prefixdb.New(validatorsDBPrefix, db)
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
	if err := inspectDB(vm.validatorsDB, "validatorsDB"); err != nil {
		return err
	}
	log.Info("Completed database inspection", "elapsed", time.Since(start))
	return nil
}

// useStandaloneDatabase returns true if the chain can and should use a standalone database
// other than given by [db] in Initialize()
func (vm *VM) useStandaloneDatabase(acceptedDB avalanchedatabase.Database) (bool, error) {
	// no config provided, use default
	standaloneDBFlag := vm.config.UseStandaloneDatabase
	if standaloneDBFlag != nil {
		return standaloneDBFlag.Bool(), nil
	}

	// check if the chain can use a standalone database
	_, err := acceptedDB.Get(lastAcceptedKey)
	if err == avalanchedatabase.ErrNotFound {
		// If there is nothing in the database, we can use the standalone database
		return true, nil
	}
	return false, err
}

// getDatabaseConfig returns the database configuration for the chain
// to be used by separate, standalone database.
func getDatabaseConfig(config config.Config, chainDataDir string) (DatabaseConfig, error) {
	var (
		configBytes []byte
		err         error
	)
	if len(config.DatabaseConfigContent) != 0 {
		dbConfigContent := config.DatabaseConfigContent
		configBytes, err = base64.StdEncoding.DecodeString(dbConfigContent)
		if err != nil {
			return DatabaseConfig{}, fmt.Errorf("unable to decode base64 content: %w", err)
		}
	} else if len(config.DatabaseConfigFile) != 0 {
		configPath := config.DatabaseConfigFile
		configBytes, err = os.ReadFile(configPath)
		if err != nil {
			return DatabaseConfig{}, err
		}
	}

	dbPath := filepath.Join(chainDataDir, "db")
	if len(config.DatabasePath) != 0 {
		dbPath = config.DatabasePath
	}

	return DatabaseConfig{
		Name:     config.DatabaseType,
		ReadOnly: config.DatabaseReadOnly,
		Path:     dbPath,
		Config:   configBytes,
	}, nil
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

func newStandaloneDatabase(dbConfig DatabaseConfig, gatherer metrics.MultiGatherer, logger logging.Logger) (avalanchedatabase.Database, error) {
	dbPath := filepath.Join(dbConfig.Path, dbConfig.Name)

	dbConfigBytes := dbConfig.Config
	// If the database is pebble, we need to set the config
	// to use no sync. Sync mode in pebble has an issue with OSs like MacOS.
	if dbConfig.Name == pebbledb.Name {
		cfg := pebbledb.DefaultConfig
		// Default to "no sync" for pebble db
		cfg.Sync = false
		if len(dbConfigBytes) > 0 {
			if err := json.Unmarshal(dbConfigBytes, &cfg); err != nil {
				return nil, err
			}
		}
		var err error
		// Marshal the config back to bytes to ensure that new defaults are applied
		dbConfigBytes, err = json.Marshal(cfg)
		if err != nil {
			return nil, err
		}
	}

	dbReg, err := metrics.MakeAndRegister(
		gatherer,
		dbMetricsPrefix,
	)
	if err != nil {
		return nil, err
	}

	db, err := factory.New(
		dbConfig.Name,
		dbPath,
		dbConfig.ReadOnly,
		dbConfigBytes,
		dbReg,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't create database: %w", err)
	}

	meterDBReg, err := metrics.MakeAndRegister(
		gatherer,
		meterDBGatherer,
	)
	if err != nil {
		return nil, err
	}

	meterDB, err := meterdb.New(meterDBReg, db)
	if err != nil {
		return nil, fmt.Errorf("couldn't create meterdb: %w", err)
	}
	return meterDB, nil
}
