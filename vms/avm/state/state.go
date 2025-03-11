// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
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
	utxoPrefix      = []byte("utxo")
	indexPrefix     = []byte("index")
	txPrefix        = []byte("tx")
	blockIDPrefix   = []byte("blockID")
	blockPrefix     = []byte("block")
	singletonPrefix = []byte("singleton")

	isInitializedKey = []byte{0x00}
	timestampKey     = []byte{0x01}
	lastAcceptedKey  = []byte{0x02}

	_ State = (*state)(nil)
)

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

	AddTx(txID ids.ID)
	AddBlock(block block.Block)
	SetLastAccepted(blkID ids.ID)
	SetTimestamp(t time.Time)
}

// State persistently maintains a set of UTXOs, transaction, statuses, and
// singletons.
type State interface {
	Chain
	avax.UTXOReader

	DeleteTx(txID ids.ID)
	DeleteBlock(blkID ids.ID)
	DeleteSingletons()

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

	UTXOs() iter.Seq2[*avax.UTXO, error]
	Txs() iter.Seq2[*txs.Tx, error]
	Blocks() iter.Seq2[block.Block, error]

	Close() error
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
	parser block.Parser
	db     *versiondb.Database

	modifiedUTXOs map[ids.ID]*avax.UTXO // map of modified UTXOID -> *UTXO if the UTXO is nil, it has been removed
	utxoDB        database.Database
	utxoState     avax.UTXOState

	addedTxs map[ids.ID]*txs.Tx            // map of txID -> *txs.Tx
	txCache  cache.Cacher[ids.ID, *txs.Tx] // cache of txID -> *txs.Tx. If the entry is nil, it is not in the database
	txDB     database.Database

	addedBlockIDs map[uint64]*ids.ID           // map of height -> blockID
	blockIDCache  cache.Cacher[uint64, ids.ID] // cache of height -> blockID. If the entry is ids.Empty, it is not in the database
	blockIDDB     database.Database

	addedBlocks map[ids.ID]*block.Block           // map of blockID -> Block
	blockCache  cache.Cacher[ids.ID, block.Block] // cache of blockID -> Block. If the entry is nil, it is not in the database
	blockDB     database.Database

	// [lastAccepted] is the most recently accepted block.
	lastAccepted          *ids.ID
	persistedLastAccepted ids.ID
	timestamp             *time.Time
	persistedTimestamp    time.Time
	singletonDB           database.Database
}

func New(
	db *versiondb.Database,
	parser block.Parser,
	metrics prometheus.Registerer,
	trackChecksums bool,
) (State, error) {
	utxoDB := prefixdb.New(utxoPrefix, db)
	txDB := prefixdb.New(txPrefix, db)
	blockIDDB := prefixdb.New(blockIDPrefix, db)
	blockDB := prefixdb.New(blockPrefix, db)
	singletonDB := prefixdb.New(singletonPrefix, db)
	return NewWithFormat(
		"",
		db,
		utxoDB,
		txDB,
		blockIDDB,
		blockDB,
		singletonDB,
		parser,
		metrics,
		trackChecksums,
	)
}

func NewWithFormat(
	namespace string,
	db *versiondb.Database,
	utxoDB database.Database,
	txDB database.Database,
	blockIDDB database.Database,
	blockDB database.Database,
	singletonDB database.Database,
	parser block.Parser,
	metrics prometheus.Registerer,
	trackChecksums bool,
) (*state, error) {
	txCache, err := metercacher.New[ids.ID, *txs.Tx](
		metric.AppendNamespace(namespace, "tx_cache"),
		metrics,
		&cache.LRU[ids.ID, *txs.Tx]{Size: txCacheSize},
	)
	if err != nil {
		return nil, err
	}

	blockIDCache, err := metercacher.New[uint64, ids.ID](
		metric.AppendNamespace(namespace, "block_id_cache"),
		metrics,
		&cache.LRU[uint64, ids.ID]{Size: blockIDCacheSize},
	)
	if err != nil {
		return nil, err
	}

	blockCache, err := metercacher.New[ids.ID, block.Block](
		metric.AppendNamespace(namespace, "block_cache"),
		metrics,
		&cache.LRU[ids.ID, block.Block]{Size: blockCacheSize},
	)
	if err != nil {
		return nil, err
	}

	utxoState, err := avax.NewMeteredUTXOState(
		namespace,
		prefixdb.New(utxoPrefix, utxoDB),
		prefixdb.New(indexPrefix, utxoDB),
		parser.Codec(),
		metrics,
		trackChecksums,
	)
	if err != nil {
		return nil, err
	}

	return &state{
		parser: parser,
		db:     db,

		modifiedUTXOs: make(map[ids.ID]*avax.UTXO),
		utxoDB:        utxoDB,
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
		return err
	}
	s.lastAccepted = lastAccepted
	s.persistedLastAccepted = lastAccepted
	s.timestamp, err = database.GetTimestamp(s.singletonDB, timestampKey)
	s.persistedTimestamp = s.timestamp
	return err
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
		return err
	}

	s.SetLastAccepted(genesis.ID())
	s.SetTimestamp(genesis.Timestamp())
	s.AddBlock(genesis)
	return s.Commit()
}

func (s *state) IsInitialized() (bool, error) {
	return s.singletonDB.Has(isInitializedKey)
}

func (s *state) SetInitialized() error {
	return s.singletonDB.Put(isInitializedKey, nil)
}

func (s *state) GetLastAccepted() ids.ID {
	return s.lastAccepted
}

func (s *state) SetLastAccepted(lastAccepted ids.ID) {
	s.lastAccepted = lastAccepted
}

func (s *state) GetTimestamp() time.Time {
	return s.timestamp
}

func (s *state) SetTimestamp(t time.Time) {
	s.timestamp = t
}

func (s *state) DeleteTx(txID ids.ID) {
	s.addedTxs[txID] = nil
}

func (s *state) DeleteBlock(blkID ids.ID) {
	s.addedBlocks[blkID] = nil
}

func (s *state) DeleteSingletons() {
	s.singletonDB
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
	s.db.Abort()
}

func (s *state) CommitBatch() (database.Batch, error) {
	if err := s.write(); err != nil {
		return nil, err
	}
	return s.db.CommitBatch()
}

func (s *state) UTXOs() iter.Seq2[*avax.UTXO, error] {
	return s.utxoState.UTXOs()
}

func (s *state) Txs() iter.Seq2[*txs.Tx, error] {
	return func(yield func(*txs.Tx, error) bool) {
		itr := s.txDB.NewIterator()
		defer itr.Release()

		for itr.Next() {
			tx, err := s.parser.ParseGenesisTx(itr.Value())
			if err != nil {
				return
			}

			if !yield(tx, errors.Join(err, itr.Error())) {
				return
			}
		}
	}
}

func (s *state) Blocks() iter.Seq2[block.Block, error] {
	return func(yield func(block.Block, error) bool) {
		itr := s.blockDB.NewIterator()
		defer itr.Release()

		for itr.Next() {
			//TODO error handling
			blk, err := s.parser.ParseBlock(itr.Value())
			if !yield(blk, errors.Join(err, itr.Error())) {
				return
			}
		}
	}
}

func (s *state) Close() error {
	return errors.Join(
		s.utxoDB.Close(),
		s.txDB.Close(),
		s.blockIDDB.Close(),
		s.blockDB.Close(),
		s.singletonDB.Close(),
		s.db.Close(),
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

		delete(s.addedTxs, txID)
		if tx != nil {
			s.txCache.Put(txID, tx)
			if err := s.txDB.Put(txID[:], txBytes); err != nil {
				return fmt.Errorf("failed to add tx: %w", err)
			}
		} else {
			if err := s.txDB.Delete(txID[:]); err != nil {
				return fmt.Errorf("failed to remove tx: %w", err)
			}
		}
	}
	return nil
}

func (s *state) writeBlockIDs() error {
	for height, blkID := range s.addedBlockIDs {
		heightKey := database.PackUInt64(height)

		delete(s.addedBlockIDs, height)

		if blkID != nil {
			s.blockIDCache.Put(height, *blkID)
			if err := database.PutID(s.blockIDDB, heightKey, *blkID); err != nil {
				return fmt.Errorf("failed to add blockID: %w", err)
			}
		} else {
			if err := s.blockIDDB.Delete(heightKey); err != nil {
				return fmt.Errorf("failed to remove blockID: %w", err)
			}
		}
	}
	return nil
}

func (s *state) writeBlocks() error {
	for blkID, blk := range s.addedBlocks {
		delete(s.addedBlocks, blkID)

		if blk != nil {
			blkBytes := (*blk).Bytes()
			s.blockCache.Put(blkID, *blk)
			if err := s.blockDB.Put(blkID[:], blkBytes); err != nil {
				return fmt.Errorf("failed to add block: %w", err)
			}
		} else {
			if err := s.blockDB.Delete(blkID[:]); err != nil {
				return fmt.Errorf("failed to remove block: %w", err)
			}
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
	return s.utxoState.Checksum(), nil
}
