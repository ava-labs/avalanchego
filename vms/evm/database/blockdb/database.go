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

	// blockDBMinHeightKey is the key for storing the minimum height config of
	// the block databases. This value determines the lowest block height that
	// will be stored in the height-indexed block databases.
	// Once set during initialization, the block database min height cannot be
	// changed without recreating the databases.
	blockDBMinHeightKey = []byte("blockdb_min_height")

	errUnexpectedKey      = errors.New("unexpected database key")
	errNotInitialized     = errors.New("database not initialized")
	errAlreadyInitialized = errors.New("database already initialized")
)

// Key prefixes for block data in [ethdb.Database].
// This is copied from libevm because they are not exported.
// Since the prefixes should never change, we can avoid libevm changes by
// duplicating them here.
var (
	kvDBHeaderPrefix    = []byte("h")
	kvDBBlockBodyPrefix = []byte("b")
	kvDBReceiptsPrefix  = []byte("r")
)

const (
	hashDataElements = 2 // number of elements in the RLP encoded block data
	blockNumberSize  = 8
	blockHashSize    = 32

	headerDBName   = "headerdb"
	bodyDBName     = "bodydb"
	receiptsDBName = "receiptsdb"
)

// Database wraps an underlying [ethdb.Database] and conditionally forwards
// block headers, bodies, and receipts to separate [database.HeightIndex] databases
// based on block height and init state. Non-block data continues to use
// the underlying [ethdb.Database] directly.
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

// New creates a new [Database] that wraps [ethdb.Database] with height-indexed
// block storage. If stateSyncEnabled is true and no existing blocks exist,
// the database will need to be initialized later via [InitBlockDBs].
func New(
	stateDB database.Database,
	kvDB ethdb.Database,
	dbPath string,
	stateSyncEnabled bool,
	config heightindexdb.DatabaseConfig,
	logger logging.Logger,
	reg prometheus.Registerer,
) (*Database, bool, error) {
	db := &Database{
		stateDB:  stateDB,
		Database: kvDB,
		dbPath:   dbPath,
		config:   config,
		reg:      reg,
		logger:   logger,
	}

	minHeight, ok, err := getDatabaseMinHeight(db.stateDB)
	if err != nil {
		return nil, false, err
	}

	// Databases already exist, load with existing min height
	if ok {
		if err := db.InitBlockDBs(minHeight); err != nil {
			return nil, false, err
		}
		return db, true, nil
	}

	// Blocks to migrate exist, initialize with min block height to migrate
	if minMigrateHeight, ok := minBlockHeightToMigrate(db.Database); ok {
		if err := db.InitBlockDBs(minMigrateHeight); err != nil {
			return nil, false, err
		}
		return db, true, nil
	}

	// No data to migrate and state sync disabled, initialize with min height 1
	if !stateSyncEnabled {
		if err := db.InitBlockDBs(1); err != nil {
			return nil, false, err
		}
		return db, true, nil
	}

	db.logger.Info(
		"Deferring block database initialization until state sync min height is known",
	)
	return db, false, nil
}

// InitBlockDBs initializes [database.HeightIndex] databases for block storage
// with the specified minimum height. This method is used when [New] defers
// initialization due to unknown min block height when state sync is enabled.
//
// Once initialized, the minimum height cannot be changed without recreating the
// databases.
//
// Returns error if already initialized.
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
		return err
	}
	receiptsDB, err := db.newMeteredHeightDB(receiptsDBName, minHeight)
	if err != nil {
		return err
	}
	db.headerDB = headerDB
	db.bodyDB = bodyDB
	db.receiptsDB = receiptsDB

	if err := db.initMigrator(); err != nil {
		return fmt.Errorf("failed to initialize migrator: %w", err)
	}

	db.heightDBsReady = true
	db.minHeight = minHeight

	db.logger.Info(
		"Initialized height-indexed block databases",
		zap.Uint64("minHeight", db.minHeight),
	)

	return nil
}

// StartMigration begins the database migration process in the background.
// This migrates block data from the key-value [ethdb.Database] to the block databases.
//
// Returns error if the block databases are not initialized.
func (db *Database) StartMigration() error {
	if !db.heightDBsReady {
		return errNotInitialized
	}
	db.migrator.start()
	return nil
}

func (db *Database) Put(key []byte, value []byte) error {
	if !db.shouldUseHeightIndexedDB(key) {
		return db.Database.Put(key, value)
	}

	heightDB, err := db.heightDBForKey(key)
	if err != nil {
		return err
	}
	num, hash, err := blockNumberAndHashFromKey(key)
	if err != nil {
		return err
	}
	return writeHashAndData(heightDB, num, hash, value)
}

func (db *Database) Get(key []byte) ([]byte, error) {
	if !db.shouldUseHeightIndexedDB(key) {
		return db.Database.Get(key)
	}

	heightDB, err := db.heightDBForKey(key)
	if err != nil {
		return nil, err
	}
	return readHashAndData(heightDB, db.Database, key, db.migrator)
}

func (db *Database) Has(key []byte) (bool, error) {
	if !db.shouldUseHeightIndexedDB(key) {
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

// Delete removes the key from the underlying database when it is not a block
// header/body/receipts key.
// Deleting block data is a no-op because [database.HeightIndex] does not support deletion.
func (db *Database) Delete(key []byte) error {
	if !db.shouldUseHeightIndexedDB(key) {
		return db.Database.Delete(key)
	}
	return nil
}

func (db *Database) Close() error {
	if db.migrator != nil {
		db.migrator.stop()
	}
	if db.heightDBsReady {
		// don't close stateDB since the caller should be managing it
		return errors.Join(
			db.headerDB.Close(),
			db.bodyDB.Close(),
			db.receiptsDB.Close(),
			db.Database.Close(),
		)
	}
	return db.Database.Close()
}

func (db *Database) initMigrator() error {
	if db.migrator != nil {
		return nil
	}
	migratorStateDB := prefixdb.New(migratorDBPrefix, db.stateDB)
	migrator, err := newMigrator(
		migratorStateDB,
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
	newDB, err := heightindexdb.New(config, db.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s database at %s: %w", namespace, path, err)
	}

	meteredDB, err := meterdb.New(db.reg, namespace, newDB)
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("failed to create metered %s database: %w", namespace, err),
			newDB.Close(),
		)
	}

	return meteredDB, nil
}

func (db *Database) heightDBForKey(key []byte) (database.HeightIndex, error) {
	switch {
	case isReceiptsKey(key):
		return db.receiptsDB, nil
	case isHeaderKey(key):
		return db.headerDB, nil
	case isBodyKey(key):
		return db.bodyDB, nil
	}
	return nil, errUnexpectedKey
}

func (db *Database) shouldUseHeightIndexedDB(key []byte) bool {
	if !db.heightDBsReady {
		return false
	}

	var n int
	switch {
	case isBodyKey(key):
		n = len(kvDBBlockBodyPrefix)
	case isHeaderKey(key):
		n = len(kvDBHeaderPrefix)
	case isReceiptsKey(key):
		n = len(kvDBReceiptsPrefix)
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
	if !b.db.shouldUseHeightIndexedDB(key) {
		return b.Batch.Put(key, value)
	}
	return b.db.Put(key, value)
}

func (b *batch) Delete(key []byte) error {
	if !b.db.shouldUseHeightIndexedDB(key) {
		return b.Batch.Delete(key)
	}
	return b.db.Delete(key)
}

func blockNumberAndHashFromKey(key []byte) (uint64, common.Hash, error) {
	var n int
	switch {
	case isBodyKey(key):
		n = len(kvDBBlockBodyPrefix)
	case isHeaderKey(key):
		n = len(kvDBHeaderPrefix)
	case isReceiptsKey(key):
		n = len(kvDBReceiptsPrefix)
	default:
		return 0, common.Hash{}, errUnexpectedKey
	}
	num := binary.BigEndian.Uint64(key[n : n+blockNumberSize])
	hash := common.BytesToHash(key[n+blockNumberSize:])
	return num, hash, nil
}

func encodeBlockNumber(number uint64) []byte {
	enc := make([]byte, blockNumberSize)
	binary.BigEndian.PutUint64(enc, number)
	return enc
}

func isBodyKey(key []byte) bool {
	if len(key) != len(kvDBBlockBodyPrefix)+blockNumberSize+blockHashSize {
		return false
	}
	return bytes.HasPrefix(key, kvDBBlockBodyPrefix)
}

func isHeaderKey(key []byte) bool {
	if len(key) != len(kvDBHeaderPrefix)+blockNumberSize+blockHashSize {
		return false
	}
	return bytes.HasPrefix(key, kvDBHeaderPrefix)
}

func isReceiptsKey(key []byte) bool {
	if len(key) != len(kvDBReceiptsPrefix)+blockNumberSize+blockHashSize {
		return false
	}
	return bytes.HasPrefix(key, kvDBReceiptsPrefix)
}

func getDatabaseMinHeight(db database.KeyValueReader) (uint64, bool, error) {
	minHeightBytes, err := db.Get(blockDBMinHeightKey)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return 0, false, nil
		}
		return 0, false, err
	}
	minHeight := binary.BigEndian.Uint64(minHeightBytes)
	return minHeight, true, nil
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
	kvDB ethdb.KeyValueReader,
	key []byte,
	migrator *migrator,
) ([]byte, error) {
	num, hash, err := blockNumberAndHashFromKey(key)
	if err != nil {
		return nil, err
	}
	data, err := heightDB.Get(num)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) && !migrator.isCompleted() {
			return kvDB.Get(key)
		}
		return nil, err
	}

	var elems [][]byte
	if err := rlp.DecodeBytes(data, &elems); err != nil {
		return nil, err
	}
	if len(elems) != hashDataElements {
		return nil, fmt.Errorf(
			"invalid hash+data format: expected %d elements, got %d",
			hashDataElements,
			len(elems),
		)
	}
	h := common.BytesToHash(elems[0])
	if h != hash {
		// Hash mismatch means we are trying to read a different block at this
		// height. Return nil data without error to indicate block not found.
		return nil, database.ErrNotFound
	}

	return elems[1], nil
}

// IsEnabled checks if the block databases has been enabled.
// Once enabled, block databases cannot be disabled.
func IsEnabled(db database.KeyValueReader) (bool, error) {
	has, err := db.Has(blockDBMinHeightKey)
	if err != nil {
		return false, err
	}
	return has, nil
}
