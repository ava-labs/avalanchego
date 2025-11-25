// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/heightindexdb/meterdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/utils/logging"

	heightindexdb "github.com/ava-labs/avalanchego/x/blockdb"
)

var (
	_ ethdb.Database = (*Database)(nil)
	_ ethdb.Batch    = (*batch)(nil)

	migratorDBPrefix = []byte("migrator")

	// blockDBMinHeightKey stores the minimum block height of the
	// height-indexed block databases.
	// It is set at initialization and cannot be changed without
	// recreating the databases.
	blockDBMinHeightKey = []byte("blockdb_min_height")

	errUnexpectedKey        = errors.New("unexpected database key")
	errNotInitialized       = errors.New("database not initialized")
	errAlreadyInitialized   = errors.New("database already initialized")
	errInvalidEncodedLength = errors.New("invalid encoded length")
)

// Key prefixes for block data in [ethdb.Database].
// This is copied from libevm because they are not exported.
// Since the prefixes should never be changed, we can avoid libevm changes by
// duplicating them here.
var (
	evmHeaderPrefix    = []byte("h")
	evmBlockBodyPrefix = []byte("b")
	evmReceiptsPrefix  = []byte("r")
)

const (
	hashDataElements = 2
	blockNumberSize  = 8
	blockHashSize    = 32

	headerDBName   = "headerdb"
	bodyDBName     = "bodydb"
	receiptsDBName = "receiptsdb"
)

// Database wraps an [ethdb.Database] and routes block headers, bodies, and receipts
// to separate [database.HeightIndex] databases for blocks at or above the minimum height.
// All other data uses the underlying [ethdb.Database] directly.
type Database struct {
	ethdb.Database

	// Databases
	stateDB    database.Database
	headerDB   database.HeightIndex
	bodyDB     database.HeightIndex
	receiptsDB database.HeightIndex

	// Configuration
	config    heightindexdb.DatabaseConfig
	dbPath    string
	minHeight uint64

	migrator       *migrator
	heightDBsReady bool

	reg    prometheus.Registerer
	logger logging.Logger
}

// New creates a new [Database] over the provided [ethdb.Database].
//
// If allowDeferredInit is true and no minimum block height is known,
// New defers initializing the height-indexed block databases until
// [Database.InitBlockDBs] is called.
//
// The bool result is true if the block databases were initialized immediately,
// and false if initialization was deferred.
func New(
	stateDB database.Database,
	evmDB ethdb.Database,
	dbPath string,
	allowDeferredInit bool,
	config heightindexdb.DatabaseConfig,
	logger logging.Logger,
	reg prometheus.Registerer,
) (*Database, bool, error) {
	db := &Database{
		stateDB:  stateDB,
		Database: evmDB,
		dbPath:   dbPath,
		config:   config,
		reg:      reg,
		logger:   logger,
	}

	minHeight, ok, err := databaseMinHeight(db.stateDB)
	if err != nil {
		return nil, false, err
	}

	// Databases already exist, load with existing min height.
	if ok {
		if err := db.InitBlockDBs(minHeight); err != nil {
			return nil, false, err
		}
		return db, true, nil
	}

	// Initialize using the minimum block height of existing blocks to migrate.
	minHeight, ok, err = minBlockHeightToMigrate(evmDB)
	if err != nil {
		return nil, false, err
	}
	if ok {
		if err := db.InitBlockDBs(minHeight); err != nil {
			return nil, false, err
		}
		return db, true, nil
	}

	// Initialize with min height 1 if deferred initialization is not allowed.
	if !allowDeferredInit {
		if err := db.InitBlockDBs(1); err != nil {
			return nil, false, err
		}
		return db, true, nil
	}

	db.logger.Info(
		"Deferring block database initialization until minimum height is known",
	)
	return db, false, nil
}

// InitBlockDBs initializes [database.HeightIndex] databases with the specified
// minimum height.
// Once initialized, the minimum height cannot be changed without recreating
// the databases.
//
// Returns an error if already initialized.
func (db *Database) InitBlockDBs(minHeight uint64) error {
	if db.heightDBsReady {
		return errAlreadyInitialized
	}

	if err := db.stateDB.Put(blockDBMinHeightKey, encodeBlockNumber(minHeight)); err != nil {
		return err
	}
	headerDB, err := db.newMeteredHeightDB(headerDBName, minHeight)
	if err != nil {
		return err
	}
	bodyDB, err := db.newMeteredHeightDB(bodyDBName, minHeight)
	if err != nil {
		return errors.Join(err, headerDB.Close())
	}
	receiptsDB, err := db.newMeteredHeightDB(receiptsDBName, minHeight)
	if err != nil {
		return errors.Join(err, headerDB.Close(), bodyDB.Close())
	}
	db.headerDB = headerDB
	db.bodyDB = bodyDB
	db.receiptsDB = receiptsDB

	if err := db.initMigrator(); err != nil {
		return errors.Join(
			fmt.Errorf("failed to initialize migrator: %w", err),
			headerDB.Close(),
			bodyDB.Close(),
			receiptsDB.Close(),
		)
	}

	db.heightDBsReady = true
	db.minHeight = minHeight

	db.logger.Info(
		"Initialized height-indexed block databases",
		zap.Uint64("minHeight", db.minHeight),
	)

	return nil
}

// StartMigration begins the background migration of block data from the
// [ethdb.Database] to the height-indexed block databases.
//
// Returns an error if the databases are not initialized.
// No error if already running.
func (db *Database) StartMigration() error {
	if !db.heightDBsReady {
		return errNotInitialized
	}
	db.migrator.start()
	return nil
}

func (db *Database) Put(key []byte, value []byte) error {
	if !db.useHeightIndexedDB(key) {
		return db.Database.Put(key, value)
	}

	heightDB, err := db.heightDBForKey(key)
	if err != nil {
		return err
	}
	num, hash, err := parseBlockKey(key)
	if err != nil {
		return err
	}
	return writeHashAndData(heightDB, num, hash, value)
}

func (db *Database) Get(key []byte) ([]byte, error) {
	if !db.useHeightIndexedDB(key) {
		return db.Database.Get(key)
	}

	heightDB, err := db.heightDBForKey(key)
	if err != nil {
		return nil, err
	}
	return readHashAndData(heightDB, db.Database, key, db.migrator)
}

func (db *Database) Has(key []byte) (bool, error) {
	if !db.useHeightIndexedDB(key) {
		return db.Database.Has(key)
	}

	_, err := db.Get(key)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Delete removes the key from the underlying database for non-block data.
// Block data deletion is a no-op because [database.HeightIndex] does not support deletion.
func (db *Database) Delete(key []byte) error {
	if !db.useHeightIndexedDB(key) {
		return db.Database.Delete(key)
	}

	num, hash, err := parseBlockKey(key)
	if err != nil {
		return err
	}
	db.logger.Warn(
		"Deleting block data is a no-op",
		zap.Uint64("height", num),
		zap.Stringer("hash", hash),
	)
	return nil
}

func (db *Database) Close() error {
	if db.migrator != nil {
		db.migrator.stop()
	}
	if !db.heightDBsReady {
		return db.Database.Close()
	}

	// Don't close stateDB since the caller should be managing it.
	return errors.Join(
		db.headerDB.Close(),
		db.bodyDB.Close(),
		db.receiptsDB.Close(),
		db.Database.Close(),
	)
}

func (db *Database) initMigrator() error {
	if db.migrator != nil {
		return nil
	}
	mdb := prefixdb.New(migratorDBPrefix, db.stateDB)
	migrator, err := newMigrator(
		mdb,
		db.headerDB,
		db.bodyDB,
		db.receiptsDB,
		db.Database,
		db.logger,
	)
	if err != nil {
		return err
	}
	db.migrator = migrator
	return nil
}

func (db *Database) newMeteredHeightDB(
	namespace string,
	minHeight uint64,
) (database.HeightIndex, error) {
	path := filepath.Join(db.dbPath, namespace)
	config := db.config.WithDir(path).WithMinimumHeight(minHeight)
	ndb, err := heightindexdb.New(config, db.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s database at %s: %w", namespace, path, err)
	}

	mdb, err := meterdb.New(db.reg, namespace, ndb)
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("failed to create metered %s database: %w", namespace, err),
			ndb.Close(),
		)
	}

	return mdb, nil
}

func (db *Database) heightDBForKey(key []byte) (database.HeightIndex, error) {
	switch {
	case isHeaderKey(key):
		return db.headerDB, nil
	case isBodyKey(key):
		return db.bodyDB, nil
	case isReceiptsKey(key):
		return db.receiptsDB, nil
	default:
		return nil, errUnexpectedKey
	}
}

func (db *Database) useHeightIndexedDB(key []byte) bool {
	if !db.heightDBsReady {
		return false
	}

	var n int
	switch {
	case isBodyKey(key):
		n = len(evmBlockBodyPrefix)
	case isHeaderKey(key):
		n = len(evmHeaderPrefix)
	case isReceiptsKey(key):
		n = len(evmReceiptsPrefix)
	default:
		return false
	}
	num := binary.BigEndian.Uint64(key[n : n+blockNumberSize])
	return num >= db.minHeight
}

type batch struct {
	ethdb.Batch
	db *Database
}

func (db *Database) NewBatch() ethdb.Batch {
	return &batch{
		db:    db,
		Batch: db.Database.NewBatch(),
	}
}

func (db *Database) NewBatchWithSize(size int) ethdb.Batch {
	return &batch{
		db:    db,
		Batch: db.Database.NewBatchWithSize(size),
	}
}

func (b *batch) Put(key []byte, value []byte) error {
	if b.db.useHeightIndexedDB(key) {
		return b.db.Put(key, value)
	}
	return b.Batch.Put(key, value)
}

func (b *batch) Delete(key []byte) error {
	if b.db.useHeightIndexedDB(key) {
		return b.db.Delete(key)
	}
	return b.Batch.Delete(key)
}

func parseBlockKey(key []byte) (num uint64, hash common.Hash, err error) {
	var n int
	switch {
	case isBodyKey(key):
		n = len(evmBlockBodyPrefix)
	case isHeaderKey(key):
		n = len(evmHeaderPrefix)
	case isReceiptsKey(key):
		n = len(evmReceiptsPrefix)
	default:
		return 0, common.Hash{}, errUnexpectedKey
	}
	num = binary.BigEndian.Uint64(key[n : n+blockNumberSize])
	bytes := key[n+blockNumberSize:]
	if len(bytes) != blockHashSize {
		return 0, common.Hash{}, fmt.Errorf("invalid hash length: %d", len(bytes))
	}
	hash = common.BytesToHash(bytes)
	return num, hash, nil
}

func encodeBlockNumber(number uint64) []byte {
	enc := make([]byte, blockNumberSize)
	binary.BigEndian.PutUint64(enc, number)
	return enc
}

func isBodyKey(key []byte) bool {
	if len(key) != len(evmBlockBodyPrefix)+blockNumberSize+blockHashSize {
		return false
	}
	return bytes.HasPrefix(key, evmBlockBodyPrefix)
}

func isHeaderKey(key []byte) bool {
	if len(key) != len(evmHeaderPrefix)+blockNumberSize+blockHashSize {
		return false
	}
	return bytes.HasPrefix(key, evmHeaderPrefix)
}

func isReceiptsKey(key []byte) bool {
	if len(key) != len(evmReceiptsPrefix)+blockNumberSize+blockHashSize {
		return false
	}
	return bytes.HasPrefix(key, evmReceiptsPrefix)
}

func databaseMinHeight(db database.KeyValueReader) (uint64, bool, error) {
	minBytes, err := db.Get(blockDBMinHeightKey)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return 0, false, nil
		}
		return 0, false, err
	}
	if len(minBytes) != blockNumberSize {
		return 0, false, fmt.Errorf("%w: min height expected %d bytes, got %d", errInvalidEncodedLength, blockNumberSize, len(minBytes))
	}
	return binary.BigEndian.Uint64(minBytes), true, nil
}

func writeHashAndData(
	db database.HeightIndex,
	height uint64,
	hash common.Hash,
	data []byte,
) error {
	encoded, err := rlp.EncodeToBytes([][]byte{hash.Bytes(), data})
	if err != nil {
		return err
	}
	return db.Put(height, encoded)
}

// readHashAndData reads data from [database.HeightIndex] and falls back
// to the [ethdb.Database] if the data is not found and migration is not complete.
func readHashAndData(
	heightDB database.HeightIndex,
	evmDB ethdb.KeyValueReader,
	key []byte,
	migrator *migrator,
) ([]byte, error) {
	num, hash, err := parseBlockKey(key)
	if err != nil {
		return nil, err
	}
	data, err := heightDB.Get(num)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) && !migrator.isCompleted() {
			return evmDB.Get(key)
		}
		return nil, err
	}

	var elems [][]byte
	if err := rlp.DecodeBytes(data, &elems); err != nil {
		return nil, err
	}
	if len(elems) != hashDataElements {
		err := fmt.Errorf(
			"invalid hash+data format: expected %d elements, got %d",
			hashDataElements,
			len(elems),
		)
		return nil, err
	}
	if common.BytesToHash(elems[0]) != hash {
		// Hash mismatch means we are trying to read a different block at this height.
		return nil, database.ErrNotFound
	}

	return elems[1], nil
}

// IsEnabled checks if blockdb has ever been initialized.
// It returns true if the minimum block height key exists, indicating the
// block database has been created and initialized with a minimum height.
func IsEnabled(db database.KeyValueReader) (bool, error) {
	has, err := db.Has(blockDBMinHeightKey)
	if err != nil {
		return false, err
	}
	return has, nil
}
