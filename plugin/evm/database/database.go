// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/ava-labs/avalanchego/api/metrics"
	avalanchenode "github.com/ava-labs/avalanchego/config/node"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/meterdb"
	"github.com/ava-labs/avalanchego/database/pebbledb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	dbMetricsPrefix = "db"
)

// createDatabase returns a new database instance with the provided configuration
func NewStandaloneDatabase(dbConfig avalanchenode.DatabaseConfig, gatherer metrics.MultiGatherer, logger logging.Logger) (database.Database, error) {
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
		dbPath := filepath.Join(dbConfig.Path, leveldb.Name)
		db, err = leveldb.New(dbPath, dbConfig.Config, logger, dbRegisterer)
		if err != nil {
			return nil, fmt.Errorf("couldn't create %s at %s: %w", leveldb.Name, dbPath, err)
		}
	case memdb.Name:
		db = memdb.New()
	case pebbledb.Name:
		dbPath := filepath.Join(dbConfig.Path, pebbledb.Name)
		db, err = NewPebbleDB(dbPath, dbConfig.Config, logger, dbRegisterer)
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

	if dbConfig.ReadOnly && dbConfig.Name != memdb.Name {
		db = versiondb.New(db)
	}

	meterDBReg, err := metrics.MakeAndRegister(
		gatherer,
		"meterdb",
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

func NewPebbleDB(file string, configBytes []byte, log logging.Logger, dbRegisterer prometheus.Registerer) (database.Database, error) {
	cfg := pebbledb.DefaultConfig
	// Use no sync for pebble db
	cfg.Sync = false
	if len(configBytes) > 0 {
		if err := json.Unmarshal(configBytes, &cfg); err != nil {
			return nil, err
		}
	}
	// Marshal the config back to bytes to ensure that new defaults are applied
	newCfgBytes, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	return pebbledb.New(file, newCfgBytes, log, dbRegisterer)
}
