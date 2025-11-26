// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/vms/avm/block"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

const (
	txCacheSize      = 8192
	blockIDCacheSize = 8192
	blockCacheSize   = 2048
)

var (
	errDBsOutOfSync = errors.New("dbs are out of sync")

	UTXOPrefix      = []byte("utxo")
	IndexPrefix     = []byte("index")
	TxPrefix        = []byte("tx")
	BlockIDPrefix   = []byte("blockID")
	BlockPrefix     = []byte("block")
	SingletonPrefix = []byte("singleton")

	isInitializedKey = []byte{0x00}
	timestampKey     = []byte{0x01}
	lastAcceptedKey  = []byte{0x02}

	_ State = (*state)(nil)
)

type VM interface {
	Replay(b block.Block) error
}

type ReadOnlyChain interface {
	avax.UTXOGetter

	GetTx(txID ids.ID) (*txs.Tx, error)
	GetBlockIDAtHeight(height uint64) (ids.ID, error)
	GetBlock(blkID ids.ID) (block.Block, error)
	GetLastAccepted() ids.ID
	GetTimestamp() time.Time
}

type Chain interface {
	ReadOnlyChain
	avax.UTXOAdder
	avax.UTXODeleter

	AddTx(tx *txs.Tx)
	AddBlock(block block.Block)
	SetLastAccepted(blkID ids.ID, height uint64)
	SetTimestamp(t time.Time)
	GetLastAcceptedHeight() uint64
}

// State persistently maintains a set of UTXOs, transaction, statuses, and
// singletons.
type State interface {
	Chain
	avax.UTXOReader

	IsInitialized() (bool, error)
	SetInitialized() error

	// InitializeChainState is called after the VM has been linearized. Calling
	// [GetLastAccepted] or [GetTimestamp] before calling this function will
	// return uninitialized data.
	//
	// Invariant: After the chain is linearized, this function is expected to be
	// called during startup.
	InitializeChainState(stopVertexID ids.ID, genesisTimestamp time.Time) error

	// Discard uncommitted changes to the database.
	Abort()

	// Commit changes to the base database.
	Commit() error

	// Returns a batch of unwritten changes that, when written, will commit all
	// pending changes to the base database.
	CommitBatch() (database.Batch, error)

	// Checksum returns the current state checksum.
	Checksum(ctx context.Context) (ids.ID, error)

	Close(ctx context.Context) error
}

/*
 * VMDB
 * |- utxos
 * | '-- utxoDB
 * |-. txs
 * | '-- txID -> tx bytes
 * |-. blockIDs
 * | '-- height -> blockID
 * |-. blocks
 * | '-- blockID -> block bytes
 * '-. singletons
 *   |-- initializedKey -> nil
 *   |-- timestampKey -> timestamp
 *   '-- lastAcceptedKey -> lastAccepted
 */
type state struct {
	parser      block.Parser
	chainDB     ChainDB
	baseLocalDB database.Database
	vdb         *versiondb.Database

	modifiedUTXOs map[ids.ID]*avax.UTXO // map of modified UTXOID -> *UTXO if the UTXO is nil, it has been removed
	utxoState     avax.UTXOState

	addedTxs map[ids.ID]*txs.Tx            // map of txID -> *txs.Tx
	txCache  cache.Cacher[ids.ID, *txs.Tx] // cache of txID -> *txs.Tx. If the entry is nil, it is not in the database
	txDB     database.Database

	addedBlockIDs map[uint64]ids.ID            // map of height -> blockID
	blockIDCache  cache.Cacher[uint64, ids.ID] // cache of height -> blockID. If the entry is ids.Empty, it is not in the database
	blockIDDB     database.Database

	addedBlocks map[ids.ID]block.Block            // map of blockID -> Block
	blockCache  cache.Cacher[ids.ID, block.Block] // cache of blockID -> Block. If the entry is nil, it is not in the database
	blockDB     database.Database

	// [lastAccepted] is the most recently accepted block.
	lastAccepted, persistedLastAccepted ids.ID
	lastAcceptedHeight uint64
	timestamp, persistedTimestamp       time.Time
	singletonDB                         database.Database
}

func New(
	baseDB database.Database,
	parser block.Parser,
	metrics prometheus.Registerer,
	trackChecksums bool,
) (*state, error) {
	vdb := versiondb.New(baseDB)

	blockDB := prefixdb.New(BlockPrefix, vdb)
	singletonDB := prefixdb.New(SingletonPrefix, vdb)
	chainDB := &NoChainDB{}

	return NewWithFormat(
		"state",
		chainDB,
		baseDB,
		vdb,
		prefixdb.New(IndexPrefix, vdb),
		prefixdb.New(TxPrefix, vdb),
		prefixdb.New(BlockIDPrefix, vdb),
		blockDB,
		singletonDB,
		avax.NewUTXODatabase(prefixdb.New(UTXOPrefix, vdb)),
		parser,
		metrics,
		trackChecksums,
	)
}

func NewWithFormat(
	namespace string,
	chainDB ChainDB,
	localBaseDB database.Database,
	vdb *versiondb.Database,
	utxoIndexDB database.Database,
	txDB database.Database,
	blockIDDB database.Database,
	blockDB database.Database,
	singletonDB database.Database,
	utxoDB avax.UTXODB,
	parser block.Parser,
	metrics prometheus.Registerer,
	trackChecksums bool,
) (*state, error) {
	txCache, err := metercacher.New[ids.ID, *txs.Tx](
		metric.AppendNamespace(namespace, "tx_cache"),
		metrics,
		lru.NewCache[ids.ID, *txs.Tx](txCacheSize),
	)
	if err != nil {
		return nil, err
	}

	blockIDCache, err := metercacher.New[uint64, ids.ID](
		metric.AppendNamespace(namespace, "block_id_cache"),
		metrics,
		lru.NewCache[uint64, ids.ID](blockIDCacheSize),
	)
	if err != nil {
		return nil, err
	}

	blockCache, err := metercacher.New[ids.ID, block.Block](
		metric.AppendNamespace(namespace, "block_cache"),
		metrics,
		lru.NewCache[ids.ID, block.Block](blockCacheSize),
	)
	if err != nil {
		return nil, err
	}

	utxoState, err := avax.NewMeteredUTXOState(
		namespace,
		utxoDB,
		utxoIndexDB,
		parser.Codec(),
		metrics,
		trackChecksums,
	)
	if err != nil {
		return nil, err
	}

	return &state{
		parser:      parser,
		chainDB:     chainDB,
		baseLocalDB: localBaseDB,
		vdb:         vdb,

		modifiedUTXOs: make(map[ids.ID]*avax.UTXO),
		utxoState:     utxoState,

		addedTxs: make(map[ids.ID]*txs.Tx),
		txCache:  txCache,
		txDB:     txDB,

		addedBlockIDs: make(map[uint64]ids.ID),
		blockIDCache:  blockIDCache,
		blockIDDB:     blockIDDB,

		addedBlocks: make(map[ids.ID]block.Block),
		blockCache:  blockCache,
		blockDB:     blockDB,

		singletonDB: singletonDB,
	}, nil
}

func (s *state) GetUTXO(utxoID ids.ID) (*avax.UTXO, error) {
	if utxo, exists := s.modifiedUTXOs[utxoID]; exists {
		if utxo == nil {
			return nil, database.ErrNotFound
		}
		return utxo, nil
	}
	return s.utxoState.GetUTXO(utxoID)
}

func (s *state) UTXOIDs(addr []byte, start ids.ID, limit int) ([]ids.ID, error) {
	return s.utxoState.UTXOIDs(addr, start, limit)
}

func (s *state) AddUTXO(utxo *avax.UTXO) {
	s.modifiedUTXOs[utxo.InputID()] = utxo
}

func (s *state) DeleteUTXO(utxoID ids.ID) {
	s.modifiedUTXOs[utxoID] = nil
}

func (s *state) GetTx(txID ids.ID) (*txs.Tx, error) {
	if tx, exists := s.addedTxs[txID]; exists {
		return tx, nil
	}
	if tx, exists := s.txCache.Get(txID); exists {
		if tx == nil {
			return nil, database.ErrNotFound
		}
		return tx, nil
	}

	txBytes, err := s.txDB.Get(txID[:])
	if err == database.ErrNotFound {
		s.txCache.Put(txID, nil)
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	// The key was in the database
	tx, err := s.parser.ParseGenesisTx(txBytes)
	if err != nil {
		return nil, err
	}

	s.txCache.Put(txID, tx)
	return tx, nil
}

func (s *state) AddTx(tx *txs.Tx) {
	txID := tx.ID()
	s.addedTxs[txID] = tx
}

func (s *state) GetBlockIDAtHeight(height uint64) (ids.ID, error) {
	if blkID, exists := s.addedBlockIDs[height]; exists {
		return blkID, nil
	}
	if blkID, cached := s.blockIDCache.Get(height); cached {
		if blkID == ids.Empty {
			return ids.Empty, database.ErrNotFound
		}

		return blkID, nil
	}

	heightKey := database.PackUInt64(height)

	blkID, err := database.GetID(s.blockIDDB, heightKey)
	if err == database.ErrNotFound {
		s.blockIDCache.Put(height, ids.Empty)
		return ids.Empty, database.ErrNotFound
	}
	if err != nil {
		return ids.Empty, err
	}

	s.blockIDCache.Put(height, blkID)
	return blkID, nil
}

func (s *state) GetBlock(blkID ids.ID) (block.Block, error) {
	if blk, exists := s.addedBlocks[blkID]; exists {
		return blk, nil
	}
	if blk, cached := s.blockCache.Get(blkID); cached {
		if blk == nil {
			return nil, database.ErrNotFound
		}

		return blk, nil
	}

	blkBytes, err := s.blockDB.Get(blkID[:])
	if err == database.ErrNotFound {
		s.blockCache.Put(blkID, nil)
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	blk, err := s.parser.ParseBlock(blkBytes)
	if err != nil {
		return nil, err
	}

	s.blockCache.Put(blkID, blk)
	return blk, nil
}

func (s *state) AddBlock(block block.Block) {
	blkID := block.ID()
	s.addedBlockIDs[block.Height()] = blkID
	s.addedBlocks[blkID] = block
}

func (s *state) InitializeChainState(stopVertexID ids.ID, genesisTimestamp time.Time) error {
	lastAccepted, err := database.GetID(s.singletonDB, lastAcceptedKey)
	if err == database.ErrNotFound {
		return s.initializeChainState(stopVertexID, genesisTimestamp)
	} else if err != nil {
		return fmt.Errorf("failed to get last accepted block: %w", err)
	}
	s.lastAccepted = lastAccepted
	s.persistedLastAccepted = lastAccepted

	timestamp, err := database.GetTimestamp(s.singletonDB, timestampKey)
	if err != nil {
		return fmt.Errorf("failed to get last accepted timestamp: %w", err)
	}

	s.timestamp = timestamp
	s.persistedTimestamp = timestamp

	return nil
}

func (s *state) initializeChainState(stopVertexID ids.ID, genesisTimestamp time.Time) error {
	genesis, err := block.NewStandardBlock(
		stopVertexID,
		0,
		genesisTimestamp,
		nil,
		s.parser.Codec(),
	)
	if err != nil {
		return fmt.Errorf("failed to initialize genesis block: %w", err)
	}

	s.SetLastAccepted(genesis.ID(), genesis.Height())
	s.SetTimestamp(genesis.Timestamp())
	s.AddBlock(genesis)

	if err := s.Commit(); err != nil {
		return fmt.Errorf("failed to commit genesis block: %w", err)
	}

	return nil
}

func (s *state) IsInitialized() (bool, error) {
	return s.singletonDB.Has(isInitializedKey)
}

func (s *state) SetInitialized() error {
	return s.singletonDB.Put(isInitializedKey, nil)
}

func (s *state) GetLastAcceptedHeight() uint64 {
	return s.lastAcceptedHeight
}

func (s *state) GetLastAccepted() ids.ID {
	return s.lastAccepted
}

func (s *state) SetLastAccepted(lastAccepted ids.ID, height uint64) {
	s.lastAccepted = lastAccepted
	s.lastAcceptedHeight = height
}

func (s *state) GetTimestamp() time.Time {
	return s.timestamp
}

func (s *state) SetTimestamp(t time.Time) {
	s.timestamp = t
}

func (s *state) Commit() error {
	defer s.Abort()
	batch, err := s.CommitBatch()
	if err != nil {
		return err
	}
	return batch.Write()
}

func (s *state) Abort() {
	s.vdb.Abort()
	s.chainDB.Abort()
}

func (s *state) CommitBatch() (database.Batch, error) {
	if err := s.write(); err != nil {
		return nil, err
	}

	b, err := s.vdb.CommitBatch()
	if err != nil {
		return nil, err
	}

	return &batch{Batch: b, state: s, chainDB: s.chainDB}, nil
}

func (s *state) Close(ctx context.Context) error {
	return errors.Join(
		s.utxoState.Close(),
		s.txDB.Close(),
		s.blockIDDB.Close(),
		s.blockDB.Close(),
		s.singletonDB.Close(),
		s.vdb.Close(),
		s.baseLocalDB.Close(),
		s.chainDB.Close(ctx),
	)
}

func (s *state) write() error {
	return errors.Join(
		s.writeUTXOs(),
		s.writeTxs(),
		s.writeBlockIDs(),
		s.writeBlocks(),
		s.writeMetadata(),
	)
}

func (s *state) writeUTXOs() error {
	for utxoID, utxo := range s.modifiedUTXOs {
		delete(s.modifiedUTXOs, utxoID)

		if utxo != nil {
			if err := s.utxoState.PutUTXO(utxo); err != nil {
				return fmt.Errorf("failed to add utxo: %w", err)
			}
		} else {
			if err := s.utxoState.DeleteUTXO(utxoID); err != nil {
				return fmt.Errorf("failed to remove utxo: %w", err)
			}
		}
	}
	return nil
}

func (s *state) writeTxs() error {
	for txID, tx := range s.addedTxs {
		txBytes := tx.Bytes()

		switch s.addedTxs[txID].Unsigned.(type) {
		case *txs.ExportTx, *txs.ImportTx:
			s.chainDB.AddAtomicTx(txID)
		default:
		}

		delete(s.addedTxs, txID)
		s.txCache.Put(txID, tx)
		if err := s.txDB.Put(txID[:], txBytes); err != nil {
			return fmt.Errorf("failed to add tx: %w", err)
		}
	}
	return nil
}

func (s *state) writeBlockIDs() error {
	for height, blkID := range s.addedBlockIDs {
		heightKey := database.PackUInt64(height)

		delete(s.addedBlockIDs, height)
		s.blockIDCache.Put(height, blkID)
		if err := database.PutID(s.blockIDDB, heightKey, blkID); err != nil {
			return fmt.Errorf("failed to add blockID: %w", err)
		}
	}
	return nil
}

func (s *state) writeBlocks() error {
	for blkID, blk := range s.addedBlocks {
		blkBytes := blk.Bytes()

		delete(s.addedBlocks, blkID)
		s.blockCache.Put(blkID, blk)
		if err := s.blockDB.Put(blkID[:], blkBytes); err != nil {
			return fmt.Errorf("failed to add block: %w", err)
		}
	}
	return nil
}

func (s *state) writeMetadata() error {
	if !s.persistedTimestamp.Equal(s.timestamp) {
		if err := database.PutTimestamp(s.singletonDB, timestampKey, s.timestamp); err != nil {
			return fmt.Errorf("failed to write timestamp: %w", err)
		}
		s.persistedTimestamp = s.timestamp
	}
	if s.persistedLastAccepted != s.lastAccepted {
		if err := database.PutID(s.singletonDB, lastAcceptedKey, s.lastAccepted); err != nil {
			return fmt.Errorf("failed to write last accepted: %w", err)
		}
		s.persistedLastAccepted = s.lastAccepted
	}
	return nil
}

func (s *state) Checksum(context.Context) (ids.ID, error) {
	return s.utxoState.Checksum()
}

type batch struct {
	database.Batch
	state   State
	chainDB ChainDB
}

// Write guarantees that after a versiondb batch is written, data in the ChainDB
// is also flushed to disk. This is to support firewood where the database is
// not shared, and we must guarantee that firewood is written to after
// versiondb.
func (b *batch) Write() error {
	chainDBHeight, ok := b.chainDB.Height()
	if !ok && b.state.GetLastAcceptedHeight() > 0 {
		// This should not be possible because we repair the chain db as part of
		// initialization
		return fmt.Errorf("chain db was not initialized")
	}

	if b.state.GetLastAcceptedHeight() != chainDBHeight {
		// Avoid writing state if heights are out of sync because it might be hiding
		// a bug that we're calling Commit more than once-per-block which would
		// create more than one state revision per block.
		return fmt.Errorf(
			"%w: state is at %d but chain db is at %d",
			errDBsOutOfSync,
			b.state.GetLastAcceptedHeight(),
			chainDBHeight,
		)
	}

	if err := b.Batch.Write(); err != nil {
		return fmt.Errorf("writing versiondb batch: %w", err)
	}

	return b.chainDB.Commit()
}
