// (c) 2020-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	repoCommitSizeCap = 10 * units.MiB
)

var (
	atomicTxIDDBPrefix         = []byte("atomicTxDB")
	atomicHeightTxDBPrefix     = []byte("atomicHeightTxDB")
	atomicRepoMetadataDBPrefix = []byte("atomicRepoMetadataDB")
	maxIndexedHeightKey        = []byte("maxIndexedAtomicTxHeight")
	bonusBlocksRepairedKey     = []byte("bonusBlocksRepaired")
)

// AtomicTxRepository defines an entity that manages storage and indexing of
// atomic transactions
type AtomicTxRepository interface {
	GetIndexHeight() (uint64, error)
	GetByTxID(txID ids.ID) (*Tx, uint64, error)
	GetByHeight(height uint64) ([]*Tx, error)
	Write(height uint64, txs []*Tx) error
	WriteBonus(height uint64, txs []*Tx) error

	IterateByHeight(start uint64) database.Iterator
	Codec() codec.Manager
}

// atomicTxRepository is a prefixdb implementation of the AtomicTxRepository interface
type atomicTxRepository struct {
	// [acceptedAtomicTxDB] maintains an index of [txID] => [height]+[atomic tx] for all accepted atomic txs.
	acceptedAtomicTxDB database.Database

	// [acceptedAtomicTxByHeightDB] maintains an index of [height] => [atomic txs] for all accepted block heights.
	acceptedAtomicTxByHeightDB database.Database

	// [atomicRepoMetadataDB] maintains a single key-value pair which tracks the height up to which the atomic repository
	// has indexed.
	atomicRepoMetadataDB database.Database

	// [db] is used to commit to the underlying versiondb.
	db *versiondb.Database

	// Use this codec for serializing
	codec codec.Manager
}

func NewAtomicTxRepository(
	db *versiondb.Database, codec codec.Manager, lastAcceptedHeight uint64,
	bonusBlocks map[uint64]ids.ID, canonicalBlocks []uint64,
	getAtomicTxFromBlockByHeight func(height uint64) (*Tx, error),
) (*atomicTxRepository, error) {
	repo := &atomicTxRepository{
		acceptedAtomicTxDB:         prefixdb.New(atomicTxIDDBPrefix, db),
		acceptedAtomicTxByHeightDB: prefixdb.New(atomicHeightTxDBPrefix, db),
		atomicRepoMetadataDB:       prefixdb.New(atomicRepoMetadataDBPrefix, db),
		codec:                      codec,
		db:                         db,
	}
	if err := repo.initializeHeightIndex(lastAcceptedHeight); err != nil {
		return nil, err
	}

	// TODO: remove post blueberry as all network participants will have applied the repair script.
	repairHeights := getAtomicRepositoryRepairHeights(bonusBlocks, canonicalBlocks)
	if err := repo.RepairForBonusBlocks(repairHeights, getAtomicTxFromBlockByHeight); err != nil {
		return nil, fmt.Errorf("failed to repair atomic repository: %w", err)
	}

	return repo, nil
}

// initializeHeightIndex initializes the atomic repository and takes care of any required migration from the previous database
// format which did not have a height -> txs index.
func (a *atomicTxRepository) initializeHeightIndex(lastAcceptedHeight uint64) error {
	startTime := time.Now()
	lastLogTime := startTime

	// [lastTxID] will be initialized to the last transaction that we indexed
	// if we are part way through a migration.
	var lastTxID ids.ID
	indexHeightBytes, err := a.atomicRepoMetadataDB.Get(maxIndexedHeightKey)
	switch err {
	case nil:
		break
	case database.ErrNotFound:
		break
	default: // unexpected value in the database
		return fmt.Errorf("found invalid value at max indexed height: %v", indexHeightBytes)
	}

	switch len(indexHeightBytes) {
	case 0:
		log.Info("Initializing atomic transaction repository from scratch")
	case common.HashLength: // partially initialized
		lastTxID, err = ids.ToID(indexHeightBytes)
		if err != nil {
			return err
		}
		log.Info("Initializing atomic transaction repository from txID", "lastTxID", lastTxID)
	case wrappers.LongLen: // already initialized
		return nil
	default: // unexpected value in the database
		return fmt.Errorf("found invalid value at max indexed height: %v", indexHeightBytes)
	}

	// Iterate from [lastTxID] to complete the re-index -> generating an index
	// from height to a slice of transactions accepted at that height
	iter := a.acceptedAtomicTxDB.NewIteratorWithStart(lastTxID[:])
	defer iter.Release()

	indexedTxs := 0

	// Keep track of the size of the currently pending writes
	pendingBytesApproximation := 0
	for iter.Next() {
		// iter.Value() consists of [height packed as uint64] + [tx serialized as packed []byte]
		iterValue := iter.Value()
		if len(iterValue) < wrappers.LongLen {
			return fmt.Errorf("atomic tx DB iterator value had invalid length (%d) < (%d)", len(iterValue), wrappers.LongLen)
		}
		heightBytes := iterValue[:wrappers.LongLen]

		// Get the tx iter is pointing to, len(txs) == 1 is expected here.
		txBytes := iterValue[wrappers.LongLen+wrappers.IntLen:]
		tx, err := ExtractAtomicTx(txBytes, a.codec)
		if err != nil {
			return err
		}

		// Check if there are already transactions at [height], to ensure that we
		// add [txs] to the already indexed transactions at [height] instead of
		// overwriting them.
		if err := a.appendTxToHeightIndex(heightBytes, tx); err != nil {
			return err
		}
		lastTxID = tx.ID()
		pendingBytesApproximation += len(txBytes)

		// call commitFn to write to underlying DB if we have reached
		// [commitSizeCap]
		if pendingBytesApproximation > repoCommitSizeCap {
			if err := a.atomicRepoMetadataDB.Put(maxIndexedHeightKey, lastTxID[:]); err != nil {
				return err
			}
			if err := a.db.Commit(); err != nil {
				return err
			}
			log.Info("Committing work initializing the atomic repository", "lastTxID", lastTxID, "pendingBytesApprox", pendingBytesApproximation)
			pendingBytesApproximation = 0
		}
		indexedTxs++
		// Periodically log progress
		if time.Since(lastLogTime) > 15*time.Second {
			lastLogTime = time.Now()
			log.Info("Atomic repository initialization", "indexedTxs", indexedTxs)
		}
	}
	if err := iter.Error(); err != nil {
		return fmt.Errorf("atomic tx DB iterator errored while initializing atomic trie: %w", err)
	}

	// Updated the value stored [maxIndexedHeightKey] to be the lastAcceptedHeight
	indexedHeight := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(indexedHeight, lastAcceptedHeight)
	if err := a.atomicRepoMetadataDB.Put(maxIndexedHeightKey, indexedHeight); err != nil {
		return err
	}

	log.Info("Completed atomic transaction repository migration", "lastAcceptedHeight", lastAcceptedHeight, "duration", time.Since(startTime))
	return a.db.Commit()
}

// GetIndexHeight returns the last height that was indexed by the atomic repository
func (a *atomicTxRepository) GetIndexHeight() (uint64, error) {
	indexHeightBytes, err := a.atomicRepoMetadataDB.Get(maxIndexedHeightKey)
	if err != nil {
		return 0, err
	}

	if len(indexHeightBytes) != wrappers.LongLen {
		return 0, fmt.Errorf("unexpected length for indexHeightBytes %d", len(indexHeightBytes))
	}
	indexHeight := binary.BigEndian.Uint64(indexHeightBytes)
	return indexHeight, nil
}

// GetByTxID queries [acceptedAtomicTxDB] for the [txID], parses a [*Tx] object
// if an entry is found, and returns it with the block height the atomic tx it
// represents was accepted on, along with an optional error.
func (a *atomicTxRepository) GetByTxID(txID ids.ID) (*Tx, uint64, error) {
	indexedTxBytes, err := a.acceptedAtomicTxDB.Get(txID[:])
	if err != nil {
		return nil, 0, err
	}

	if len(indexedTxBytes) < wrappers.LongLen {
		return nil, 0, fmt.Errorf("acceptedAtomicTxDB entry too short: %d", len(indexedTxBytes))
	}

	// value is stored as [height]+[tx bytes], decompose with a packer.
	packer := wrappers.Packer{Bytes: indexedTxBytes}
	height := packer.UnpackLong()
	txBytes := packer.UnpackBytes()
	tx, err := ExtractAtomicTx(txBytes, a.codec)
	if err != nil {
		return nil, 0, err
	}

	return tx, height, nil
}

// GetByHeight returns all atomic txs processed on block at [height].
// Returns [database.ErrNotFound] if there are no atomic transactions indexed at [height].
// Note: if [height] is below the last accepted height, then this means that there were
// no atomic transactions in the block accepted at [height].
// If [height] is greater than the last accepted height, then this will always return
// [database.ErrNotFound]
func (a *atomicTxRepository) GetByHeight(height uint64) ([]*Tx, error) {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)

	return a.getByHeightBytes(heightBytes)
}

func (a *atomicTxRepository) getByHeightBytes(heightBytes []byte) ([]*Tx, error) {
	txsBytes, err := a.acceptedAtomicTxByHeightDB.Get(heightBytes)
	if err != nil {
		return nil, err
	}
	return ExtractAtomicTxsBatch(txsBytes, a.codec)
}

// Write updates indexes maintained on atomic txs, so they can be queried
// by txID or height. This method must be called only once per height,
// and [txs] must include all atomic txs for the block accepted at the
// corresponding height.
func (a *atomicTxRepository) Write(height uint64, txs []*Tx) error {
	return a.write(height, txs, false)
}

// WriteBonus is similar to Write, except the [txID] => [height] is not
// overwritten if already exists.
func (a *atomicTxRepository) WriteBonus(height uint64, txs []*Tx) error {
	return a.write(height, txs, true)
}

func (a *atomicTxRepository) write(height uint64, txs []*Tx, bonus bool) error {
	if len(txs) > 1 {
		// txs should be stored in order of txID to ensure consistency
		// with txs initialized from the txID index.
		copyTxs := make([]*Tx, len(txs))
		copy(copyTxs, txs)
		sort.Slice(copyTxs, func(i, j int) bool { return copyTxs[i].ID().Hex() < copyTxs[j].ID().Hex() })
		txs = copyTxs
	}
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)
	// Skip adding an entry to the height index if [txs] is empty.
	if len(txs) > 0 {
		for _, tx := range txs {
			if bonus {
				switch _, _, err := a.GetByTxID(tx.ID()); err {
				case nil:
					// avoid overwriting existing value if [bonus] is true
					continue
				case database.ErrNotFound:
					// no existing value to overwrite, proceed as normal
				default:
					// unexpected error
					return err
				}
			}
			if err := a.indexTxByID(heightBytes, tx); err != nil {
				return err
			}
		}
		if err := a.indexTxsAtHeight(heightBytes, txs); err != nil {
			return err
		}
	}

	// Update the index height regardless of if any atomic transactions
	// were present at [height].
	return a.atomicRepoMetadataDB.Put(maxIndexedHeightKey, heightBytes)
}

// indexTxByID writes [tx] into the [acceptedAtomicTxDB] stored as
// [height] + [tx bytes]
func (a *atomicTxRepository) indexTxByID(heightBytes []byte, tx *Tx) error {
	txBytes, err := a.codec.Marshal(codecVersion, tx)
	if err != nil {
		return err
	}

	// map txID => [height]+[tx bytes]
	heightTxPacker := wrappers.Packer{Bytes: make([]byte, wrappers.LongLen+wrappers.IntLen+len(txBytes))}
	heightTxPacker.PackFixedBytes(heightBytes)
	heightTxPacker.PackBytes(txBytes)
	txID := tx.ID()

	if err := a.acceptedAtomicTxDB.Put(txID[:], heightTxPacker.Bytes); err != nil {
		return err
	}

	return nil
}

// indexTxsAtHeight adds [height] -> [txs] to the [acceptedAtomicTxByHeightDB]
func (a *atomicTxRepository) indexTxsAtHeight(heightBytes []byte, txs []*Tx) error {
	txsBytes, err := a.codec.Marshal(codecVersion, txs)
	if err != nil {
		return err
	}
	if err := a.acceptedAtomicTxByHeightDB.Put(heightBytes, txsBytes); err != nil {
		return err
	}
	return nil
}

// appendTxToHeightIndex retrieves the transactions stored at [heightBytes] and appends
// [tx] to the slice of transactions stored there.
// This function is used while initializing the atomic repository to re-index the atomic transactions
// by txID into the height -> txs index.
func (a *atomicTxRepository) appendTxToHeightIndex(heightBytes []byte, tx *Tx) error {
	txs, err := a.getByHeightBytes(heightBytes)
	if err != nil && err != database.ErrNotFound {
		return err
	}

	// Iterate over the existing transactions to ensure we do not add a
	// duplicate to the index.
	for _, existingTx := range txs {
		if existingTx.ID() == tx.ID() {
			return nil
		}
	}

	txs = append(txs, tx)
	return a.indexTxsAtHeight(heightBytes, txs)
}

// IterateByHeight returns an iterator beginning at [height].
// Note [height] must be greater than 0 since we assume there are no
// atomic txs in genesis.
func (a *atomicTxRepository) IterateByHeight(height uint64) database.Iterator {
	heightBytes := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(heightBytes, height)
	return a.acceptedAtomicTxByHeightDB.NewIteratorWithStart(heightBytes)
}

func (a *atomicTxRepository) Codec() codec.Manager {
	return a.codec
}

func (a *atomicTxRepository) isBonusBlocksRepaired() (bool, error) {
	return a.atomicRepoMetadataDB.Has(bonusBlocksRepairedKey)
}

func (a *atomicTxRepository) markBonusBlocksRepaired(repairedEntries uint64) error {
	val := make([]byte, wrappers.LongLen)
	binary.BigEndian.PutUint64(val, repairedEntries)
	return a.atomicRepoMetadataDB.Put(bonusBlocksRepairedKey, val)
}

// RepairForBonusBlocks ensures that atomic txs that were processed on more than one block
// (canonical block + a number of bonus blocks) are indexed to the first height they were
// processed on (canonical block). [sortedHeights] should include all canonical block and
// bonus block heights in ascending order, and will only be passed as non-empty on mainnet.
func (a *atomicTxRepository) RepairForBonusBlocks(
	sortedHeights []uint64, getAtomicTxFromBlockByHeight func(height uint64) (*Tx, error),
) error {
	done, err := a.isBonusBlocksRepaired()
	if err != nil {
		return err
	}
	if done {
		return nil
	}
	repairedEntries := uint64(0)
	seenTxs := make(map[ids.ID][]uint64)
	for _, height := range sortedHeights {
		// get atomic tx from block
		tx, err := getAtomicTxFromBlockByHeight(height)
		if err != nil {
			return err
		}
		if tx == nil {
			continue
		}

		// get the tx by txID and update it, the first time we encounter
		// a given [txID], overwrite the previous [txID] => [height]
		// mapping. This provides a canonical mapping across nodes.
		heights, seen := seenTxs[tx.ID()]
		_, foundHeight, err := a.GetByTxID(tx.ID())
		if err != nil && !errors.Is(err, database.ErrNotFound) {
			return err
		}
		if !seen {
			if err := a.Write(height, []*Tx{tx}); err != nil {
				return err
			}
		} else {
			if err := a.WriteBonus(height, []*Tx{tx}); err != nil {
				return err
			}
		}
		if foundHeight != height && !seen {
			repairedEntries++
		}
		seenTxs[tx.ID()] = append(heights, height)
	}
	if err := a.markBonusBlocksRepaired(repairedEntries); err != nil {
		return err
	}
	log.Info("atomic tx repository RepairForBonusBlocks complete", "repairedEntries", repairedEntries)
	return a.db.Commit()
}

// getAtomicRepositoryRepairHeights returns a slice containing heights from bonus blocks and
// canonical blocks sorted by height.
func getAtomicRepositoryRepairHeights(bonusBlocks map[uint64]ids.ID, canonicalBlocks []uint64) []uint64 {
	repairHeights := make([]uint64, 0, len(bonusBlocks)+len(canonicalBlocks))
	for height := range bonusBlocks {
		repairHeights = append(repairHeights, height)
	}
	for _, height := range canonicalBlocks {
		// avoid appending duplicates
		if _, exists := bonusBlocks[height]; !exists {
			repairHeights = append(repairHeights, height)
		}
	}
	sort.Slice(repairHeights, func(i, j int) bool { return repairHeights[i] < repairHeights[j] })
	return repairHeights
}
