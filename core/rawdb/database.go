// (c) 2019-2020, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2018 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package rawdb

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ava-labs/libevm/common"
	ethrawdb "github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/ethdb/leveldb"
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/ava-labs/libevm/ethdb/pebble"
	"github.com/ava-labs/libevm/log"
)

// nofreezedb is a database wrapper that disables freezer data retrievals.
type nofreezedb struct {
	ethdb.KeyValueStore
}

// HasAncient returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) HasAncient(kind string, number uint64) (bool, error) {
	return false, errNotSupported
}

// Ancient returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) Ancient(kind string, number uint64) ([]byte, error) {
	return nil, errNotSupported
}

// AncientRange returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) AncientRange(kind string, start, max, maxByteSize uint64) ([][]byte, error) {
	return nil, errNotSupported
}

// Ancients returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) Ancients() (uint64, error) {
	return 0, errNotSupported
}

// Tail returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) Tail() (uint64, error) {
	return 0, errNotSupported
}

// AncientSize returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) AncientSize(kind string) (uint64, error) {
	return 0, errNotSupported
}

// ModifyAncients is not supported.
func (db *nofreezedb) ModifyAncients(func(ethdb.AncientWriteOp) error) (int64, error) {
	return 0, errNotSupported
}

// TruncateHead returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) TruncateHead(items uint64) (uint64, error) {
	return 0, errNotSupported
}

// TruncateTail returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) TruncateTail(items uint64) (uint64, error) {
	return 0, errNotSupported
}

// Sync returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) Sync() error {
	return errNotSupported
}

func (db *nofreezedb) ReadAncients(fn func(reader ethdb.AncientReaderOp) error) (err error) {
	// Unlike other ancient-related methods, this method does not return
	// errNotSupported when invoked.
	// The reason for this is that the caller might want to do several things:
	// 1. Check if something is in the freezer,
	// 2. If not, check leveldb.
	//
	// This will work, since the ancient-checks inside 'fn' will return errors,
	// and the leveldb work will continue.
	//
	// If we instead were to return errNotSupported here, then the caller would
	// have to explicitly check for that, having an extra clause to do the
	// non-ancient operations.
	return fn(db)
}

// MigrateTable processes the entries in a given table in sequence
// converting them to a new format if they're of an old format.
func (db *nofreezedb) MigrateTable(kind string, convert convertLegacyFn) error {
	return errNotSupported
}

// AncientDatadir returns an error as we don't have a backing chain freezer.
func (db *nofreezedb) AncientDatadir() (string, error) {
	return "", errNotSupported
}

// NewDatabase creates a high level database on top of a given key-value data
// store without a freezer moving immutable chain segments into cold storage.
func NewDatabase(db ethdb.KeyValueStore) ethdb.Database {
	return &nofreezedb{KeyValueStore: db}
}

// NewMemoryDatabase creates an ephemeral in-memory key-value database without a
// freezer moving immutable chain segments into cold storage.
func NewMemoryDatabase() ethdb.Database {
	return NewDatabase(memorydb.New())
}

// NewMemoryDatabaseWithCap creates an ephemeral in-memory key-value database
// with an initial starting capacity, but without a freezer moving immutable
// chain segments into cold storage.
func NewMemoryDatabaseWithCap(size int) ethdb.Database {
	return NewDatabase(memorydb.NewWithCap(size))
}

// NewLevelDBDatabase creates a persistent key-value database without a freezer
// moving immutable chain segments into cold storage.
func NewLevelDBDatabase(file string, cache int, handles int, namespace string, readonly bool) (ethdb.Database, error) {
	db, err := leveldb.New(file, cache, handles, namespace, readonly)
	if err != nil {
		return nil, err
	}
	log.Info("Using LevelDB as the backing database")
	return NewDatabase(db), nil
}

// NewPebbleDBDatabase creates a persistent key-value database without a freezer
// moving immutable chain segments into cold storage.
func NewPebbleDBDatabase(file string, cache int, handles int, namespace string, readonly, ephemeral bool) (ethdb.Database, error) {
	db, err := pebble.New(file, cache, handles, namespace, readonly, ephemeral)
	if err != nil {
		return nil, err
	}
	return NewDatabase(db), nil
}

const (
	dbPebble  = "pebble"
	dbLeveldb = "leveldb"
)

// PreexistingDatabase checks the given data directory whether a database is already
// instantiated at that location, and if so, returns the type of database (or the
// empty string).
func PreexistingDatabase(path string) string {
	if _, err := os.Stat(filepath.Join(path, "CURRENT")); err != nil {
		return "" // No pre-existing db
	}
	if matches, err := filepath.Glob(filepath.Join(path, "OPTIONS*")); len(matches) > 0 || err != nil {
		if err != nil {
			panic(err) // only possible if the pattern is malformed
		}
		return dbPebble
	}
	return dbLeveldb
}

// OpenOptions contains the options to apply when opening a database.
// OBS: If AncientsDirectory is empty, it indicates that no freezer is to be used.
type OpenOptions struct {
	Type      string // "leveldb" | "pebble"
	Directory string // the datadir
	Namespace string // the namespace for database relevant metrics
	Cache     int    // the capacity(in megabytes) of the data caching
	Handles   int    // number of files to be open simultaneously
	ReadOnly  bool
	// Ephemeral means that filesystem sync operations should be avoided: data integrity in the face of
	// a crash is not important. This option should typically be used in tests.
	Ephemeral bool
}

// openKeyValueDatabase opens a disk-based key-value database, e.g. leveldb or pebble.
//
//	                      type == null          type != null
//	                   +----------------------------------------
//	db is non-existent |  pebble default  |  specified type
//	db is existent     |  from db         |  specified type (if compatible)
func openKeyValueDatabase(o OpenOptions) (ethdb.Database, error) {
	// Reject any unsupported database type
	if len(o.Type) != 0 && o.Type != dbLeveldb && o.Type != dbPebble {
		return nil, fmt.Errorf("unknown db.engine %v", o.Type)
	}
	// Retrieve any pre-existing database's type and use that or the requested one
	// as long as there's no conflict between the two types
	existingDb := PreexistingDatabase(o.Directory)
	if len(existingDb) != 0 && len(o.Type) != 0 && o.Type != existingDb {
		return nil, fmt.Errorf("db.engine choice was %v but found pre-existing %v database in specified data directory", o.Type, existingDb)
	}
	if o.Type == dbPebble || existingDb == dbPebble {
		log.Info("Using pebble as the backing database")
		return NewPebbleDBDatabase(o.Directory, o.Cache, o.Handles, o.Namespace, o.ReadOnly, o.Ephemeral)
	}
	if o.Type == dbLeveldb || existingDb == dbLeveldb {
		log.Info("Using leveldb as the backing database")
		return NewLevelDBDatabase(o.Directory, o.Cache, o.Handles, o.Namespace, o.ReadOnly)
	}
	// No pre-existing database, no user-requested one either. Default to Pebble.
	log.Info("Defaulting to pebble as the backing database")
	return NewPebbleDBDatabase(o.Directory, o.Cache, o.Handles, o.Namespace, o.ReadOnly, o.Ephemeral)
}

// Open opens both a disk-based key-value database such as leveldb or pebble, but also
// integrates it with a freezer database -- if the AncientDir option has been
// set on the provided OpenOptions.
// The passed o.AncientDir indicates the path of root ancient directory where
// the chain freezer can be opened.
func Open(o OpenOptions) (ethdb.Database, error) {
	kvdb, err := openKeyValueDatabase(o)
	if err != nil {
		return nil, err
	}
	return kvdb, nil
}

// InspectDatabase traverses the entire database and checks the size
// of all different categories of data.
func InspectDatabase(db ethdb.Database, keyPrefix, keyStart []byte) error {
	var (
		codeToFetch   ethrawdb.DatabaseStat
		syncPerformed ethrawdb.DatabaseStat
		syncProgress  ethrawdb.DatabaseStat
		syncSegments  ethrawdb.DatabaseStat
	)

	options := []ethrawdb.InspectDatabaseOption{
		ethrawdb.WithDatabaseMetadataKeys(func(key []byte) bool {
			return bytes.Equal(key, snapshotBlockHashKey) ||
				bytes.Equal(key, syncRootKey)
		}),
		ethrawdb.WithDatabaseStatRecorder(func(key []byte, size common.StorageSize) bool {
			switch {
			case bytes.HasPrefix(key, syncSegmentsPrefix) && len(key) == syncSegmentsKeyLength:
				syncSegments.Add(size)
				return true
			case bytes.HasPrefix(key, syncStorageTriesPrefix) && len(key) == syncStorageTriesKeyLength:
				syncProgress.Add(size)
				return true
			case bytes.HasPrefix(key, CodeToFetchPrefix) && len(key) == codeToFetchKeyLength:
				codeToFetch.Add(size)
				return true
			case bytes.HasPrefix(key, syncPerformedPrefix) && len(key) == syncPerformedKeyLength:
				syncPerformed.Add(size)
				return true
			default:
				return false
			}
		}),
		ethrawdb.WithDatabaseStatsTransformer(func(rows [][]string) [][]string {
			newRows := make([][]string, 0, len(rows))
			for _, row := range rows {
				database := row[0]
				category := row[1]
				switch {
				case database == "Key-Value store" && category == "Difficulties",
					database == "Key-Value store" && category == "Beacon sync headers",
					database == "Ancient store (Chain)":
					// Discard rows specific to libevm (geth) but irrelevant to coreth.
					continue
				}
				newRows = append(newRows, row)
			}

			return append(
				newRows,
				[]string{"State sync", "Trie segments", syncSegments.Size(), syncSegments.Count()},
				[]string{"State sync", "Storage tries to fetch", syncProgress.Size(), syncProgress.Count()},
				[]string{"State sync", "Code to fetch", codeToFetch.Size(), codeToFetch.Count()},
				[]string{"State sync", "Block numbers synced to", syncPerformed.Size(), syncPerformed.Count()},
			)
		}),
	}

	return ethrawdb.InspectDatabase(db, keyPrefix, keyStart, options...)
}

// ClearPrefix removes all keys in db that begin with prefix and match an
// expected key length. [keyLen] should include the length of the prefix.
func ClearPrefix(db ethdb.KeyValueStore, prefix []byte, keyLen int) error {
	it := db.NewIterator(prefix, nil)
	defer it.Release()

	batch := db.NewBatch()
	for it.Next() {
		key := common.CopyBytes(it.Key())
		if len(key) != keyLen {
			// avoid deleting keys that do not match the expected length
			continue
		}
		if err := batch.Delete(key); err != nil {
			return err
		}
		if batch.ValueSize() > ethdb.IdealBatchSize {
			if err := batch.Write(); err != nil {
				return err
			}
			batch.Reset()
		}
	}
	if err := it.Error(); err != nil {
		return err
	}
	return batch.Write()
}

/// TODO: Consider adding ReadChainMetadata
