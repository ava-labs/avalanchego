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
	"github.com/ava-labs/avalanchego/utils/logging"

	heightindexdb "github.com/ava-labs/avalanchego/x/blockdb"
)

var (
	errUnexpectedKey        = errors.New("unexpected database key")
	errNotInitialized       = errors.New("database not initialized")
	errAlreadyInitialized   = errors.New("database already initialized")
	errInvalidEncodedLength = errors.New("invalid encoded length")
)

var _ ethdb.Database = (*Database)(nil)

// Database wraps an [ethdb.Database] and routes block headers, bodies, and receipts
// to separate [database.HeightIndex] databases for blocks at or above the minimum height.
// All other data uses the underlying [ethdb.Database] directly.
type Database struct {
	ethdb.Database

	// Databases
	metaDB     database.Database
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

const blockNumberSize = 8

func encodeBlockNumber(number uint64) []byte {
	enc := make([]byte, blockNumberSize)
	binary.BigEndian.PutUint64(enc, number)
	return enc
}

// blockDBMinHeightKey stores the minimum block height of the
// height-indexed block databases.
// It is set at initialization and cannot be changed without
// recreating the databases.
var blockDBMinHeightKey = []byte("blockdb_min_height")

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

// IsEnabled checks if blockdb has ever been initialized.
// It returns true if the minimum block height key exists, indicating the
// block databases have been created and initialized with a minimum height.
func IsEnabled(db database.KeyValueReader) (bool, error) {
	has, err := db.Has(blockDBMinHeightKey)
	if err != nil {
		return false, err
	}
	return has, nil
}

func (db *Database) newMeteredHeightDB(
	namespace string,
	minHeight uint64,
) (database.HeightIndex, error) {
	path := filepath.Join(db.dbPath, namespace)
	config := db.config.WithDir(path).WithMinimumHeight(minHeight)
	ndb, err := heightindexdb.New(config, logging.NoLog{})
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

// New creates a new [Database] over the provided [ethdb.Database].
//
// If allowDeferredInit is true and no minimum block height is known,
// New defers initializing the height-indexed block databases until
// [Database.InitBlockDBs] is called.
//
// The bool result is true if the block databases were initialized immediately,
// and false if initialization was deferred.
func New(
	metaDB database.Database,
	evmDB ethdb.Database,
	dbPath string,
	allowDeferredInit bool,
	config heightindexdb.DatabaseConfig,
	logger logging.Logger,
	reg prometheus.Registerer,
) (*Database, bool, error) {
	db := &Database{
		metaDB:   metaDB,
		Database: evmDB,
		dbPath:   dbPath,
		config:   config,
		reg:      reg,
		logger:   logger,
	}

	minHeightFn := [](func() (uint64, bool, error)){
		func() (uint64, bool, error) {
			// Load existing database min height.
			return databaseMinHeight(db.metaDB)
		},
		func() (uint64, bool, error) {
			// Use the minimum block height of existing blocks to migrate.
			return minBlockHeightToMigrate(evmDB)
		},
		func() (uint64, bool, error) {
			// Use min height 1 unless deferring initialization.
			return 1, !allowDeferredInit, nil
		},
	}
	for _, fn := range minHeightFn {
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

	if err := db.metaDB.Put(blockDBMinHeightKey, encodeBlockNumber(minHeight)); err != nil {
		return err
	}
	headerDB, err := db.newMeteredHeightDB("headerdb", minHeight)
	if err != nil {
		return err
	}
	bodyDB, err := db.newMeteredHeightDB("bodydb", minHeight)
	if err != nil {
		return errors.Join(err, headerDB.Close())
	}
	receiptsDB, err := db.newMeteredHeightDB("receiptsdb", minHeight)
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

// Key prefixes for block data in [ethdb.Database].
// This is copied from libevm because they are not exported.
// Since the prefixes should never be changed, we can avoid libevm changes by
// duplicating them here.
const (
	evmHeaderPrefix    = 'h'
	evmBlockBodyPrefix = 'b'
	evmReceiptsPrefix  = 'r'
)

var blockPrefixes = []byte{evmBlockBodyPrefix, evmHeaderPrefix, evmReceiptsPrefix}

func parseBlockKey(key []byte) (num uint64, hash common.Hash, ok bool) {
	// Block keys should have 1 byte prefix + blockNumberSize + 32 bytes for the hash
	if len(key) != 1+blockNumberSize+32 {
		return 0, common.Hash{}, false
	}
	if !slices.Contains(blockPrefixes, key[0]) {
		return 0, common.Hash{}, false
	}
	num = binary.BigEndian.Uint64(key[1 : 1+blockNumberSize])
	bytes := key[1+blockNumberSize:]
	hash = common.BytesToHash(bytes)
	return num, hash, true
}

type parsedBlockKey struct {
	key  []byte
	db   database.HeightIndex
	num  uint64
	hash common.Hash
}

func (p *parsedBlockKey) writeHashAndData(data []byte) error {
	return writeHashAndData(p.db, p.num, p.hash, data)
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

// parseKey parses a block key into a parsedBlockKey.
// It returns false if no block databases for the key prefix exist.
func (db *Database) parseKey(key []byte) (*parsedBlockKey, bool) {
	if !db.heightDBsReady {
		return nil, false
	}

	var hdb database.HeightIndex
	switch key[0] {
	case evmBlockBodyPrefix:
		hdb = db.bodyDB
	case evmHeaderPrefix:
		hdb = db.headerDB
	case evmReceiptsPrefix:
		hdb = db.receiptsDB
	default:
		return nil, false
	}

	num, hash, ok := parseBlockKey(key)
	if !ok {
		return nil, false
	}

	if num < db.minHeight {
		return nil, false
	}

	return &parsedBlockKey{
		key:  key,
		db:   hdb,
		num:  num,
		hash: hash,
	}, true
}

// readBlock reads data from [database.HeightIndex] and falls back
// to the [ethdb.Database] if the data is not found and migration is not complete.
func (db *Database) readBlock(p *parsedBlockKey) ([]byte, error) {
	data, err := p.db.Get(p.num)
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
	if len(elems) != 2 {
		err := fmt.Errorf(
			"invalid hash+data format: expected 2 elements, got %d", len(elems),
		)
		return nil, err
	}

	// Hash mismatch means we are trying to read a different block at this height.
	if common.BytesToHash(elems[0]) != p.hash {
		return nil, database.ErrNotFound
	}

	return elems[1], nil
}

func (db *Database) Get(key []byte) ([]byte, error) {
	if p, ok := db.parseKey(key); ok {
		return db.readBlock(p)
	}
	return db.Database.Get(key)
}

func (db *Database) Put(key []byte, value []byte) error {
	if p, ok := db.parseKey(key); ok {
		return p.writeHashAndData(value)
	}
	return db.Database.Put(key, value)
}

func (db *Database) Has(key []byte) (bool, error) {
	p, ok := db.parseKey(key)
	if !ok {
		return db.Database.Has(key)
	}

	if _, err := db.readBlock(p); err != nil {
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
	if _, ok := db.parseKey(key); ok {
		return nil
	}
	return db.Database.Delete(key)
}

func (db *Database) Close() error {
	if db.migrator != nil {
		db.migrator.stop()
	}
	if !db.heightDBsReady {
		return db.Database.Close()
	}

	// Don't close metaDB since the caller should be managing it.
	return errors.Join(
		db.headerDB.Close(),
		db.bodyDB.Close(),
		db.receiptsDB.Close(),
		db.Database.Close(),
	)
}

var _ ethdb.Batch = (*batch)(nil)

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
	if p, ok := b.db.parseKey(key); ok {
		return p.writeHashAndData(value)
	}
	return b.Batch.Put(key, value)
}

func (b *batch) Delete(key []byte) error {
	if _, ok := b.db.parseKey(key); ok {
		return b.db.Delete(key)
	}
	return b.Batch.Delete(key)
}
