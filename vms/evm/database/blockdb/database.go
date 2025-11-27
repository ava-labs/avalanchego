// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/heightindexdb/meterdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/x/blockdb"
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
const (
	evmHeaderPrefix    = 'h'
	evmBlockBodyPrefix = 'b'
	evmReceiptsPrefix  = 'r'
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
	config    blockdb.DatabaseConfig
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
	config blockdb.DatabaseConfig,
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

	minHeightSrcs := [](func() (uint64, bool, error)){
		func() (uint64, bool, error) {
			// If databases already exist, load with existing min height.
			return databaseMinHeight(db.stateDB)
		},
		func() (uint64, bool, error) {
			// Initialize using the minimum block height of existing blocks to migrate.
			return minBlockHeightToMigrate(evmDB)
		},
		func() (uint64, bool, error) {
			return 1, !allowDeferredInit, nil
		},
	}
	for _, fn := range minHeightSrcs {
		h, ok, err := fn()
		if err != nil {
			return nil, false, err
		}
		if !ok {
			continue
		}
		if err := db.InitBlockDBs(h); err != nil {
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
	p, ok := db.parseBlockKey(key)
	if !ok {
		return db.Database.Put(key, value)
	}
	return p.writeHashAndData(value)
}

func (db *Database) Get(key []byte) ([]byte, error) {
	p, ok := db.parseBlockKey(key)
	if !ok {
		return db.Database.Get(key)
	}
	return db.readHashAndData(p)
}

func (db *Database) Has(key []byte) (bool, error) {
	p, ok := db.parseBlockKey(key)
	if !ok {
		return db.Database.Has(key)
	}

	switch _, err := db.readHashAndData(p); {
	case errors.Is(err, database.ErrNotFound):
		return false, nil
	case err != nil:
		return false, err
	}
	return true, nil
}

// Delete removes the key from the underlying database for non-block data.
// Block data deletion is a no-op because [database.HeightIndex] does not support deletion.
func (db *Database) Delete(key []byte) error {
	p, ok := db.parseBlockKey(key)
	if !ok {
		return db.Database.Delete(key)
	}
	db.logger.Warn(
		"Deleting block data is a no-op",
		zap.Uint64("height", p.blockNum),
		zap.Stringer("hash", p.blockHash),
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
	ndb, err := blockdb.New(config, db.logger)
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
	if p, ok := b.db.parseBlockKey(key); ok {
		return p.writeHashAndData(value)
	}
	return b.Batch.Put(key, value)
}

func (b *batch) Delete(key []byte) error {
	if _, ok := b.db.parseBlockKey(key); ok {
		return b.db.Delete(key)
	}
	return b.Batch.Delete(key)
}

func parseBlockKey(key []byte) (num uint64, hash common.Hash, ok bool) {
	// All valid prefixes have length 1.
	if len(key) != 1+blockNumberSize+blockHashSize {
		return 0, hash, false
	}
	if !slices.Contains([]byte{evmBlockBodyPrefix, evmHeaderPrefix, evmReceiptsPrefix}, key[0]) {
		return 0, common.Hash{}, false
	}
	num = binary.BigEndian.Uint64(key[1 : 1+blockNumberSize])
	bytes := key[1+blockNumberSize:]
	hash = common.BytesToHash(bytes)
	return num, hash, true
}

type parsedBlockKey struct {
	key       []byte
	heightDB  database.HeightIndex
	blockNum  uint64
	blockHash common.Hash
}

func (db *Database) parseBlockKey(key []byte) (*parsedBlockKey, bool) {
	if !db.heightDBsReady {
		return nil, false
	}
	num, hash, ok := parseBlockKey(key)
	if !ok {
		return nil, false
	}
	if num < db.minHeight {
		return nil, false
	}
	p := &parsedBlockKey{
		key:       key,
		blockNum:  num,
		blockHash: hash,
	}

	switch key[0] {
	case evmBlockBodyPrefix:
		p.heightDB = db.bodyDB
	case evmHeaderPrefix:
		p.heightDB = db.headerDB
	case evmReceiptsPrefix:
		p.heightDB = db.receiptsDB
	default:
		return nil, false
	}

	return p, true
}

func encodeBlockNumber(number uint64) []byte {
	enc := make([]byte, blockNumberSize)
	binary.BigEndian.PutUint64(enc, number)
	return enc
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

func (p *parsedBlockKey) writeHashAndData(data []byte) error {
	return writeHashAndData(p.heightDB, p.blockNum, p.blockHash, data)
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
func (db *Database) readHashAndData(p *parsedBlockKey) ([]byte, error) {
	data, err := p.heightDB.Get(p.blockNum)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) && !db.migrator.isCompleted() {
			return db.Database.Get(p.key)
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
	if common.BytesToHash(elems[0]) != p.blockHash {
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
