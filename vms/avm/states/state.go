// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package states

import (
	"bytes"
	"fmt"
	"sync"
	"time"

	stdmath "math"

	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/avm/blocks"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

const (
	statusCacheSize  = 8192
	txCacheSize      = 8192
	blockIDCacheSize = 8192
	blockCacheSize   = 2048

	pruneCommitLimit           = 1024
	pruneCommitSleepMultiplier = 5
	pruneUpdateFrequency       = 30 * time.Second
)

var (
	utxoPrefix      = []byte("utxo")
	statusPrefix    = []byte("status")
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
	GetBlockID(height uint64) (ids.ID, error)
	GetBlock(blkID ids.ID) (blocks.Block, error)
	GetLastAccepted() ids.ID
	GetTimestamp() time.Time
}

type Chain interface {
	ReadOnlyChain
	avax.UTXOAdder
	avax.UTXODeleter

	AddTx(tx *txs.Tx)
	AddBlock(block blocks.Block)
	SetLastAccepted(blkID ids.ID)
	SetTimestamp(t time.Time)
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

	// TODO: deprecate statuses. We should only persist accepted state
	// Status returns a status from storage.
	GetStatus(id ids.ID) (choices.Status, error)
	// AddStatus saves a status in storage.
	AddStatus(id ids.ID, status choices.Status)

	// Discard uncommitted changes to the database.
	Abort()

	// Commit changes to the base database.
	Commit() error

	// Returns a batch of unwritten changes that, when written, will commit all
	// pending changes to the base database.
	CommitBatch() (database.Batch, error)

	// Asynchronously removes unneeded state from disk.
	//
	// Specifically, this removes:
	// - All transaction statuses
	// - All non-accepted transactions
	// - All UTXOs that were consumed by accepted transactions
	//
	// [lock] is the AVM's context lock and is assumed to be unlocked when this
	// method is called.
	//
	// TODO: remove after v1.11.x is activated
	Prune(lock sync.Locker, log logging.Logger) error

	Close() error
}

/*
 * VMDB
 * |- utxos
 * | '-- utxoDB
 * |- statuses
 * | '-- statusDB
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
	parser blocks.Parser
	db     *versiondb.Database

	modifiedUTXOs map[ids.ID]*avax.UTXO // map of modified UTXOID -> *UTXO if the UTXO is nil, it has been removed
	utxoDB        database.Database
	utxoState     avax.UTXOState

	statusesPruned bool
	addedStatuses  map[ids.ID]choices.Status
	statusCache    cache.Cacher[ids.ID, *choices.Status] // cache of id -> choices.Status. If the entry is nil, it is not in the database
	statusDB       database.Database

	addedTxs map[ids.ID]*txs.Tx            // map of txID -> *txs.Tx
	txCache  cache.Cacher[ids.ID, *txs.Tx] // cache of txID -> *txs.Tx. If the entry is nil, it is not in the database
	txDB     database.Database

	addedBlockIDs map[uint64]ids.ID            // map of height -> blockID
	blockIDCache  cache.Cacher[uint64, ids.ID] // cache of height -> blockID. If the entry is ids.Empty, it is not in the database
	blockIDDB     database.Database

	addedBlocks map[ids.ID]blocks.Block            // map of blockID -> Block
	blockCache  cache.Cacher[ids.ID, blocks.Block] // cache of blockID -> Block. If the entry is nil, it is not in the database
	blockDB     database.Database

	// [lastAccepted] is the most recently accepted block.
	lastAccepted, persistedLastAccepted ids.ID
	timestamp, persistedTimestamp       time.Time
	singletonDB                         database.Database
}

func New(
	db *versiondb.Database,
	parser blocks.Parser,
	metrics prometheus.Registerer,
) (State, error) {
	utxoDB := prefixdb.New(utxoPrefix, db)
	statusDB := prefixdb.New(statusPrefix, db)
	txDB := prefixdb.New(txPrefix, db)
	blockIDDB := prefixdb.New(blockIDPrefix, db)
	blockDB := prefixdb.New(blockPrefix, db)
	singletonDB := prefixdb.New(singletonPrefix, db)

	statusCache, err := metercacher.New[ids.ID, *choices.Status](
		"status_cache",
		metrics,
		&cache.LRU[ids.ID, *choices.Status]{Size: statusCacheSize},
	)
	if err != nil {
		return nil, err
	}

	txCache, err := metercacher.New[ids.ID, *txs.Tx](
		"tx_cache",
		metrics,
		&cache.LRU[ids.ID, *txs.Tx]{Size: txCacheSize},
	)
	if err != nil {
		return nil, err
	}

	blockIDCache, err := metercacher.New[uint64, ids.ID](
		"block_id_cache",
		metrics,
		&cache.LRU[uint64, ids.ID]{Size: blockIDCacheSize},
	)
	if err != nil {
		return nil, err
	}

	blockCache, err := metercacher.New[ids.ID, blocks.Block](
		"block_cache",
		metrics,
		&cache.LRU[ids.ID, blocks.Block]{Size: blockCacheSize},
	)
	if err != nil {
		return nil, err
	}

	utxoState, err := avax.NewMeteredUTXOState(utxoDB, parser.Codec(), metrics)
	return &state{
		parser: parser,
		db:     db,

		modifiedUTXOs: make(map[ids.ID]*avax.UTXO),
		utxoDB:        utxoDB,
		utxoState:     utxoState,

		addedStatuses: make(map[ids.ID]choices.Status),
		statusCache:   statusCache,
		statusDB:      statusDB,

		addedTxs: make(map[ids.ID]*txs.Tx),
		txCache:  txCache,
		txDB:     txDB,

		addedBlockIDs: make(map[uint64]ids.ID),
		blockIDCache:  blockIDCache,
		blockIDDB:     blockIDDB,

		addedBlocks: make(map[ids.ID]blocks.Block),
		blockCache:  blockCache,
		blockDB:     blockDB,

		singletonDB: singletonDB,
	}, err
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
	s.addedTxs[tx.ID()] = tx
}

func (s *state) GetBlockID(height uint64) (ids.ID, error) {
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

func (s *state) GetBlock(blkID ids.ID) (blocks.Block, error) {
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

func (s *state) AddBlock(block blocks.Block) {
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
	genesis, err := blocks.NewStandardBlock(
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

// TODO: remove status support
func (s *state) GetStatus(id ids.ID) (choices.Status, error) {
	if s.statusesPruned {
		return choices.Unknown, database.ErrNotFound
	}

	if status, exists := s.addedStatuses[id]; exists {
		return status, nil
	}
	if status, found := s.statusCache.Get(id); found {
		if status == nil {
			return choices.Unknown, database.ErrNotFound
		}
		return *status, nil
	}

	val, err := database.GetUInt32(s.statusDB, id[:])
	if err == database.ErrNotFound {
		s.statusCache.Put(id, nil)
		return choices.Unknown, database.ErrNotFound
	}
	if err != nil {
		return choices.Unknown, err
	}

	status := choices.Status(val)
	if err := status.Valid(); err != nil {
		return choices.Unknown, err
	}
	s.statusCache.Put(id, &status)
	return status, nil
}

// TODO: remove status support
func (s *state) AddStatus(id ids.ID, status choices.Status) {
	s.addedStatuses[id] = status
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

func (s *state) Close() error {
	errs := wrappers.Errs{}
	errs.Add(
		s.utxoDB.Close(),
		s.statusDB.Close(),
		s.txDB.Close(),
		s.blockIDDB.Close(),
		s.blockDB.Close(),
		s.singletonDB.Close(),
		s.db.Close(),
	)
	return errs.Err
}

func (s *state) write() error {
	errs := wrappers.Errs{}
	errs.Add(
		s.writeUTXOs(),
		s.writeTxs(),
		s.writeBlockIDs(),
		s.writeBlocks(),
		s.writeMetadata(),
		s.writeStatuses(),
	)
	return errs.Err
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
		txID := txID
		txBytes := tx.Bytes()

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
		blkID := blkID
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

func (s *state) writeStatuses() error {
	for id, status := range s.addedStatuses {
		id := id
		status := status

		delete(s.addedStatuses, id)
		s.statusCache.Put(id, &status)
		if err := database.PutUInt32(s.statusDB, id[:], uint32(status)); err != nil {
			return fmt.Errorf("failed to add status: %w", err)
		}
	}
	return nil
}

func (s *state) Prune(lock sync.Locker, log logging.Logger) error {
	lock.Lock()
	// It is possible that more txs are added after grabbing this iterator. No
	// new txs will write a status, so we don't need to check those txs.
	statusIter := s.statusDB.NewIterator()
	// Releasing is done using a closure to ensure that updating statusIter will
	// result in having the most recent iterator released when executing the
	// deferred function.
	defer func() {
		statusIter.Release()
	}()

	if !statusIter.Next() {
		// If there are no statuses on disk, pruning was previously run and
		// finished.
		lock.Unlock()

		log.Info("state already pruned")

		return statusIter.Error()
	}

	startTxIDBytes := statusIter.Key()
	txIter := s.txDB.NewIteratorWithStart(startTxIDBytes)
	// Releasing is done using a closure to ensure that updating statusIter will
	// result in having the most recent iterator released when executing the
	// deferred function.
	defer func() {
		txIter.Release()
	}()

	// While we are pruning the disk, we disable caching of the data we are
	// modifying. Caching is re-enabled when pruning finishes.
	//
	// Note: If an unexpected error occurs the caches are never re-enabled.
	// That's fine as the node is going to be in an unhealthy state regardless.
	oldTxCache := s.txCache
	s.statusCache = &cache.Empty[ids.ID, *choices.Status]{}
	s.txCache = &cache.Empty[ids.ID, *txs.Tx]{}
	lock.Unlock()

	startTime := time.Now()
	lastCommit := startTime
	lastUpdate := startTime
	startProgress := timer.ProgressFromHash(startTxIDBytes)

	startStatusBytes := statusIter.Value()
	if err := s.cleanupTx(lock, startTxIDBytes, startStatusBytes, txIter); err != nil {
		return err
	}

	i := 1
	for statusIter.Next() {
		txIDBytes := statusIter.Key()
		statusBytes := statusIter.Value()
		if err := s.cleanupTx(lock, txIDBytes, statusBytes, txIter); err != nil {
			return err
		}

		i++

		if i%pruneCommitLimit == 0 {
			// We must hold the lock during committing to make sure we don't
			// attempt to commit to disk while a block is concurrently being
			// accepted.
			lock.Lock()
			errs := wrappers.Errs{}
			errs.Add(
				s.Commit(),
				statusIter.Error(),
				txIter.Error(),
			)
			lock.Unlock()
			if errs.Errored() {
				return errs.Err
			}

			// We release the iterators here to allow the underlying database to
			// clean up deleted state.
			statusIter.Release()
			txIter.Release()

			now := time.Now()
			if now.Sub(lastUpdate) > pruneUpdateFrequency {
				lastUpdate = now

				progress := timer.ProgressFromHash(txIDBytes)
				eta := timer.EstimateETA(
					startTime,
					progress-startProgress,
					stdmath.MaxUint64-startProgress,
				)
				log.Info("committing state pruning",
					zap.Int("count", i),
					zap.Duration("eta", eta),
				)
			}

			// We take the minimum here because it's possible that the node is
			// currently bootstrapping. This would mean that grabbing the lock
			// could take an extremely long period of time; which we should not
			// delay processing for.
			pruneDuration := now.Sub(lastCommit)
			sleepDuration := math.Min(
				pruneCommitSleepMultiplier*pruneDuration,
				10*time.Second,
			)
			time.Sleep(sleepDuration)

			// Make sure not to include the sleep duration into the next prune
			// duration.
			lastCommit = time.Now()

			// We shouldn't need to grab the lock here, but doing so ensures
			// that we see a consistent view across both the statusDB and the
			// txDB.
			lock.Lock()
			statusIter = s.statusDB.NewIteratorWithStart(txIDBytes)
			txIter = s.txDB.NewIteratorWithStart(txIDBytes)
			lock.Unlock()
		}
	}

	lock.Lock()
	defer lock.Unlock()

	errs := wrappers.Errs{}
	errs.Add(
		s.Commit(),
		statusIter.Error(),
		txIter.Error(),
	)

	// Make sure we flush the original cache before re-enabling it to prevent
	// surfacing any stale data.
	oldTxCache.Flush()
	s.statusesPruned = true
	s.txCache = oldTxCache

	log.Info("finished state pruning",
		zap.Int("count", i),
		zap.Duration("duration", time.Since(startTime)),
	)

	return errs.Err
}

// Assumes [lock] is unlocked.
func (s *state) cleanupTx(lock sync.Locker, txIDBytes []byte, statusBytes []byte, txIter database.Iterator) error {
	// After the linearization, we write txs to disk without statuses to mark
	// them as accepted. This means that there may be more txs than statuses and
	// we need to skip over them.
	//
	// Note: We do not need to remove UTXOs consumed after the linearization, as
	// those UTXOs are guaranteed to have already been deleted.
	if err := skipTo(txIter, txIDBytes); err != nil {
		return err
	}
	// txIter.Key() is now `txIDBytes`

	statusInt, err := database.ParseUInt32(statusBytes)
	if err != nil {
		return err
	}
	status := choices.Status(statusInt)

	if status == choices.Accepted {
		txBytes := txIter.Value()
		tx, err := s.parser.ParseGenesisTx(txBytes)
		if err != nil {
			return err
		}

		utxos := tx.Unsigned.InputUTXOs()

		// Locking is done here to make sure that any concurrent verification is
		// performed with a valid view of the state.
		lock.Lock()
		defer lock.Unlock()

		// Remove all the UTXOs consumed by the accepted tx. Technically we only
		// need to remove UTXOs consumed by operations, but it's easy to just
		// remove all of them.
		for _, UTXO := range utxos {
			if err := s.utxoState.DeleteUTXO(UTXO.InputID()); err != nil {
				return err
			}
		}
	} else {
		lock.Lock()
		defer lock.Unlock()

		// This tx wasn't accepted, so we can remove it entirely from disk.
		if err := s.txDB.Delete(txIDBytes); err != nil {
			return err
		}
	}
	// By removing the status, we will treat the tx as accepted if it is still
	// on disk.
	return s.statusDB.Delete(txIDBytes)
}

// skipTo advances [iter] until its key is equal to [targetKey]. If [iter] does
// not contain [targetKey] an error will be returned.
//
// Note: [iter.Next()] will always be called at least once.
func skipTo(iter database.Iterator, targetKey []byte) error {
	for {
		if !iter.Next() {
			return fmt.Errorf("%w: 0x%x", database.ErrNotFound, targetKey)
		}
		key := iter.Key()
		switch bytes.Compare(targetKey, key) {
		case -1:
			return fmt.Errorf("%w: 0x%x", database.ErrNotFound, targetKey)
		case 0:
			return nil
		}
	}
}
