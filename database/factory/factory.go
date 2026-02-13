// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package factory

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/corruptabledb"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/pebbledb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// New creates a new database instance based on the provided configuration.
//
// It also wraps the database with a corruptable DB.
//
// dbName is the name of the database, either leveldb, memdb, or pebbledb.
// dbPath is the path to the database folder.
// readOnly indicates if the database should be read-only.
// dbConfig is the database configuration in JSON format.
func New(
	name string,
	path string,
	readOnly bool,
	config []byte,
	reg prometheus.Registerer,
	logger logging.Logger,
) (database.Database, error) {
	var (
		db  database.Database
		err error
	)
	switch name {
	case leveldb.Name:
		db, err = leveldb.New(path, config, logger, reg)
	case memdb.Name:
		db = memdb.New()
	case pebbledb.Name:
		db, err = pebbledb.New(path, config, logger, reg)
	default:
		err = fmt.Errorf(
			"db-type must be one of {%s, %s, %s}",
			leveldb.Name,
			memdb.Name,
			pebbledb.Name,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("couldn't create %q at %q: %w", name, path, err)
	}

	db = corruptabledb.New(db, logger)
	if readOnly && name != memdb.Name {
		db = versiondb.New(db)
	}
	return db, nil
}
