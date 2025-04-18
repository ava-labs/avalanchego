// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package factory

import (
	"fmt"
	"path/filepath"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/corruptabledb"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/meterdb"
	"github.com/ava-labs/avalanchego/database/pebbledb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/utils/logging"
)

type DatabaseConfig struct {
	// If true, all writes are to memory and are discarded at node shutdown.
	ReadOnly bool `json:"readOnly"`

	// Path to database
	Path string `json:"path"`

	// Name of the database type to use
	Name string `json:"name"`

	// Path to config file
	Config []byte `json:"-"`
}

// NewDatabase creates a new database instance based on the provided configuration.
// It supports LevelDB, MemDB, and PebbleDB as database types.
// It also wraps the database with a corruptable DB and a meter DB.
// [dbFolderName] is used to create the database path, it's appended to the [dbConfig.Path] to get the full path.
// [dbMetricsPrefix] is used to create a new metrics registerer for the database.
// [meterDBRegName] is used to create a new metrics registerer for the meter DB.
func NewDatabase(dbConfig DatabaseConfig, gatherer metrics.MultiGatherer, logger logging.Logger, dbFolderName, dbMetricsPrefix, meterDBRegName string) (database.Database, error) {
	dbRegisterer, err := metrics.MakeAndRegister(
		gatherer,
		dbMetricsPrefix,
	)
	if err != nil {
		return nil, err
	}
	var db database.Database
	// start the db
	switch dbConfig.Name {
	case leveldb.Name:
		dbPath := filepath.Join(dbConfig.Path, dbFolderName)
		db, err = leveldb.New(dbPath, dbConfig.Config, logger, dbRegisterer)
		if err != nil {
			return nil, fmt.Errorf("couldn't create %s at %s: %w", leveldb.Name, dbPath, err)
		}
	case memdb.Name:
		db = memdb.New()
	case pebbledb.Name:
		dbPath := filepath.Join(dbConfig.Path, dbFolderName)
		db, err = pebbledb.New(dbPath, dbConfig.Config, logger, dbRegisterer)
		if err != nil {
			return nil, fmt.Errorf("couldn't create %s at %s: %w", pebbledb.Name, dbPath, err)
		}
	default:
		return nil, fmt.Errorf(
			"db-type was %q but should have been one of {%s, %s, %s}",
			dbConfig.Name,
			leveldb.Name,
			memdb.Name,
			pebbledb.Name,
		)
	}

	// Wrap with corruptable DB
	db = corruptabledb.New(db)

	if dbConfig.ReadOnly && dbConfig.Name != memdb.Name {
		db = versiondb.New(db)
	}

	meterDBReg, err := metrics.MakeAndRegister(
		gatherer,
		meterDBRegName,
	)
	if err != nil {
		return nil, err
	}

	db, err = meterdb.New(meterDBReg, db)
	if err != nil {
		return nil, fmt.Errorf("failed to create meterdb: %w", err)
	}

	return db, nil
}
