package semanticdb

import (
	"fmt"
	"os"
	"path"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/meterdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/prometheus/client_golang/prometheus"
)

// Database wraps a database and its prior version in order
// to simplify database upgrades
// semanticdb can be wrapped in prefixdbs using New and NewNested
// which will produce equivalent prefixdbs in both the base database
// and the PriorDB.
// However, if one layer is created using versiondb or a prefixdb
// outside of this package, then access to the priorDB will be lost.
type Database struct {
	database.Database

	PriorDB database.Database

	// Note: versions are unspecified if the Database is created
	// using the Create function
	Major, Minor, Patch                int
	PriorMajor, PriorMinor, PriorPatch int
}

// CreateFromPath ...
func CreateFromPath(dbDirPath string, major, minor, patch int) (database.Database, error) {
	if major < 0 || minor < 0 || patch < 0 {
		return nil, fmt.Errorf("invalid db version: v%d.%d.%d", major, minor, patch)
	}

	dbVersion := fmt.Sprintf("v%d.%d.%d", major, minor, patch)
	dbPath := path.Join(dbDirPath, dbVersion)
	db, err := leveldb.New(dbPath, 0, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("couldn't create db at %s: %w", dbPath, err)
	}
	// Check for most recent semantic version (with matching major version)
	// where the database already exists.
	var priorDB database.Database
	for i := minor; i >= 0; i-- {
		for j := patch - 1; j >= 0; j-- {
			priorVersionPath := path.Join(dbDirPath, fmt.Sprintf("v%d.%d.%d", major, i, j))
			if _, err := os.Stat(priorVersionPath); os.IsNotExist(err) {
				continue
			}
			priorDB, err = leveldb.New(priorVersionPath, 0, 0, 0)
			if err != nil {
				return nil, fmt.Errorf("couldn't create db for prior db version at %s: %w", priorVersionPath, err)
			}
			return &Database{
				Database:   db,
				PriorDB:    priorDB,
				Major:      major,
				Minor:      minor,
				Patch:      patch,
				PriorMajor: major,
				PriorMinor: i,
				PriorPatch: j,
			}, nil
		}
	}

	return db, nil
}

// Create ...
func Create(db database.Database, priorDB database.Database) database.Database {
	if priorDB == nil {
		return db
	}

	return &Database{
		Database: db,
		PriorDB:  priorDB,
	}
}

// New ...
func New(prefix []byte, db database.Database) database.Database {
	if semanticDB, ok := db.(*Database); ok {
		return &Database{
			Database:   prefixdb.New(prefix, semanticDB.Database),
			PriorDB:    prefixdb.New(prefix, semanticDB.PriorDB),
			Major:      semanticDB.Major,
			Minor:      semanticDB.Minor,
			Patch:      semanticDB.Patch,
			PriorMajor: semanticDB.PriorMajor,
			PriorMinor: semanticDB.PriorMinor,
			PriorPatch: semanticDB.PriorPatch,
		}
	}
	return prefixdb.New(prefix, db)
}

// NewNested ...
func NewNested(prefix []byte, db database.Database) database.Database {
	if semanticDB, ok := db.(*Database); ok {
		return &Database{
			Database:   prefixdb.NewNested(prefix, semanticDB.Database),
			PriorDB:    prefixdb.NewNested(prefix, semanticDB.PriorDB),
			Major:      semanticDB.Major,
			Minor:      semanticDB.Minor,
			Patch:      semanticDB.Patch,
			PriorMajor: semanticDB.PriorMajor,
			PriorMinor: semanticDB.PriorMinor,
			PriorPatch: semanticDB.PriorPatch,
		}
	}
	return prefixdb.NewNested(prefix, db)
}

// NewMeterDB attempts to wrap a semantic database in a metered DB
func NewMeterDB(namespace string, registerer prometheus.Registerer, db database.Database) (database.Database, error) {
	if semanticDB, ok := db.(*Database); ok {
		meterDB, err := meterdb.New(namespace, registerer, semanticDB.Database)
		if err != nil {
			return nil, err
		}
		return &Database{
			Database:   meterDB,
			PriorDB:    semanticDB.PriorDB,
			Major:      semanticDB.Major,
			Minor:      semanticDB.Minor,
			Patch:      semanticDB.Patch,
			PriorMajor: semanticDB.PriorMajor,
			PriorMinor: semanticDB.PriorMinor,
			PriorPatch: semanticDB.PriorPatch,
		}, nil
	}

	return meterdb.New(namespace, registerer, db)
}

// Close ...
func (db *Database) Close() error {
	errs := wrappers.Errs{}
	errs.Add(
		db.Database.Close(),
		db.PriorDB.Close(),
	)

	return errs.Err
}
