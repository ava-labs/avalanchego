// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package manager

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ava-labs/avalanchego/utils"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/meterdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	errNonSortedAndUniqueDBs = errors.New("managed databases were not sorted and unique")
)

type Manager interface {
	// Current returns the database with the current database version
	Current() *SemanticDatabase
	// Last returns the last database prior to the current database or an error if none exists
	Last() (*SemanticDatabase, bool)
	// GetDatabases returns all the managed databases in order from current to the oldest version
	GetDatabases() []*SemanticDatabase
	// Close closes all of the databases
	Close() error

	NewPrefixDBManager([]byte) Manager
	NewNestedPrefixDBManager([]byte) Manager
	NewMeterDBManager(string, prometheus.Registerer) (Manager, error)
}

type manager struct {
	databases []*SemanticDatabase
}

func (m *manager) Current() *SemanticDatabase { return m.databases[0] }

func (m *manager) Last() (*SemanticDatabase, bool) {
	if len(m.databases) < 2 {
		return nil, false
	}
	return m.databases[1], true
}

func (m *manager) GetDatabases() []*SemanticDatabase { return m.databases }

func (m *manager) Close() error {
	errs := wrappers.Errs{}

	for _, db := range m.databases {
		errs.Add(db.Close())
	}

	return errs.Err
}

// wrapManager returns a new database manager with each managed database wrapped by
// the [wrap] function. If an error is returned by wrap, the error is returned
// immediately. If [wrap] never returns an error, then wrapManager is guaranteed to
// never return an error.
func (m *manager) wrapManager(wrap func(db *SemanticDatabase) (*SemanticDatabase, error)) (*manager, error) {
	databases := make([]*SemanticDatabase, len(m.databases))
	for i, db := range m.databases {
		wrappedDB, err := wrap(db)
		if err != nil {
			return nil, err
		}
		databases[i] = wrappedDB
	}
	return &manager{databases: databases}, nil
}

// NewDefaultMemDBManager returns a database manager with a single memory db instance
// with a default version of v1.0.0
func NewDefaultMemDBManager() Manager {
	return &manager{
		databases: []*SemanticDatabase{
			{
				Database: memdb.New(),
				Version:  version.NewDefaultVersion(1, 0, 0),
			},
		},
	}
}

// New creates a database manager at [filePath] by creating a database instance from each directory
// and is less than or equal to the specified version
func New(dbDirPath string, currentVersion version.Version) (Manager, error) {
	parser := version.NewDefaultParser()

	currentDBPath := path.Join(dbDirPath, currentVersion.String())
	currentDB, err := leveldb.New(currentDBPath, 0, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("couldn't create db at %s: %w", currentDBPath, err)
	}

	semDBs := make([]*SemanticDatabase, 1)
	semDBs[0] = &SemanticDatabase{
		Database: currentDB,
		Version:  currentVersion,
	}
	err = filepath.Walk(dbDirPath, func(path string, info os.FileInfo, err error) error {
		// Skip the root directory
		if path == dbDirPath {
			return nil
		}

		// The database directory should only contain database directories, no files.
		if !info.IsDir() {
			return fmt.Errorf("unexpectedly found non-directory at %s", path)
		}
		_, dbName := filepath.Split(path)
		version, err := parser.Parse(dbName)
		if err != nil {
			return err
		}

		// If [version] is greater than or equal to the specified version
		// skip over creating the new database to avoid creating the same db
		// twice or creating a database with a version ahead of the desired one.
		if cmp := version.Compare(currentVersion); cmp >= 0 {
			return filepath.SkipDir
		}

		db, err := leveldb.New(path, 0, 0, 0)
		if err != nil {
			return fmt.Errorf("couldn't create db at %s: %w", path, err)
		}

		semDBs = append(semDBs, &SemanticDatabase{
			Database: db,
			Version:  version,
		})

		return filepath.SkipDir
	})

	SortDescending(semDBs)

	m := &manager{
		databases: semDBs,
	}

	// If an error occurred, close all of the opened databases
	// and return the original error
	if err != nil {
		_ = m.Close()
		return nil, err
	}

	return m, nil
}

// NewPrefixDBManager creates a new manager with each database instance prefixed
// by [prefix]
func (m *manager) NewPrefixDBManager(prefix []byte) Manager {
	m, _ = m.wrapManager(func(sdb *SemanticDatabase) (*SemanticDatabase, error) {
		return &SemanticDatabase{
			Database: prefixdb.New(prefix, sdb.Database),
			Version:  sdb.Version,
		}, nil
	})
	return m
}

// NewNestedPrefixDBManager creates a new manager with each database instance
// wrapped with a nested prfix of [prefix]
func (m *manager) NewNestedPrefixDBManager(prefix []byte) Manager {
	m, _ = m.wrapManager(func(sdb *SemanticDatabase) (*SemanticDatabase, error) {
		return &SemanticDatabase{
			Database: prefixdb.NewNested(prefix, sdb.Database),
			Version:  sdb.Version,
		}, nil
	})
	return m
}

// NewMeterDBManager wraps each database instance with a meterdb instance. The namespace
// is concatenated with the version of the database. Note: calling this more than once
// with the same [namespace] more than once will cause a conflict for the [registerer]
// and produce an error.
func (m *manager) NewMeterDBManager(namespace string, registerer prometheus.Registerer) (Manager, error) {
	return m.wrapManager(func(sdb *SemanticDatabase) (*SemanticDatabase, error) {
		mdb, err := meterdb.New(fmt.Sprintf("%s_%s", namespace, strings.ReplaceAll(sdb.Version.String(), ".", "_")), registerer, sdb.Database)
		if err != nil {
			return nil, err
		}
		return &SemanticDatabase{
			Database: mdb,
			Version:  sdb.Version,
		}, nil
	})
}

// NewManagerFromDBs
func NewManagerFromDBs(dbs []*SemanticDatabase) (Manager, error) {
	SortDescending(dbs)
	sortedAndUnique := utils.IsSortedAndUnique(innerSortDescendingSemanticDBs(dbs))
	if !sortedAndUnique {
		return nil, errNonSortedAndUniqueDBs
	}
	return &manager{
		databases: dbs,
	}, nil
}

type SemanticDatabase struct {
	database.Database

	version.Version
}

type innerSortDescendingSemanticDBs []*SemanticDatabase

// Less returns true if the version at index i is greater than the version at index j
// such that it will sort in descending order
func (dbs innerSortDescendingSemanticDBs) Less(i, j int) bool {
	return dbs[i].Version.Compare(dbs[j].Version) > 0
}

func (dbs innerSortDescendingSemanticDBs) Len() int      { return len(dbs) }
func (dbs innerSortDescendingSemanticDBs) Swap(i, j int) { dbs[j], dbs[i] = dbs[i], dbs[j] }

func SortDescending(dbs []*SemanticDatabase) { sort.Sort(innerSortDescendingSemanticDBs(dbs)) }
