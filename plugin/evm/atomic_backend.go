// (c) 2020-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	syncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

var _ AtomicBackend = &atomicBackend{}

// AtomicBackend abstracts the verification and processing
// of atomic transactions
type AtomicBackend interface {
	// InsertTxs calculates the root of the atomic trie that would
	// result from applying [txs] to the atomic trie, starting at the state
	// corresponding to previously verified block [parentHash].
	// If [blockHash] is provided, the modified atomic trie is pinned in memory
	// and it's the caller's responsibility to call either Accept or Reject on
	// the AtomicState which can be retreived from GetVerifiedAtomicState to commit the
	// changes or abort them and free memory.
	InsertTxs(blockHash common.Hash, blockHeight uint64, parentHash common.Hash, txs []*Tx) (common.Hash, error)

	// Returns an AtomicState corresponding to a block hash that has been inserted
	// but not Accepted or Rejected yet.
	GetVerifiedAtomicState(blockHash common.Hash) (AtomicState, error)

	// AtomicTrie returns the atomic trie managed by this backend.
	AtomicTrie() AtomicTrie

	// ApplyToSharedMemory applies the atomic operations that have been indexed into the trie
	// but not yet applied to shared memory for heights less than or equal to [lastAcceptedBlock].
	// This executes operations in the range [cursorHeight+1, lastAcceptedBlock].
	// The cursor is initially set by  MarkApplyToSharedMemoryCursor to signal to the atomic trie
	// the range of operations that were added to the trie without being executed on shared memory.
	ApplyToSharedMemory(lastAcceptedBlock uint64) error

	// MarkApplyToSharedMemoryCursor marks the atomic trie as containing atomic ops that
	// have not been executed on shared memory starting at [previousLastAcceptedHeight+1].
	// This is used when state sync syncs the atomic trie, such that the atomic operations
	// from [previousLastAcceptedHeight+1] to the [lastAcceptedHeight] set by state sync
	// will not have been executed on shared memory.
	MarkApplyToSharedMemoryCursor(previousLastAcceptedHeight uint64) error

	// Syncer creates and returns a new Syncer object that can be used to sync the
	// state of the atomic trie from peers
	Syncer(client syncclient.LeafClient, targetRoot common.Hash, targetHeight uint64) (Syncer, error)

	// SetLastAccepted is used after state-sync to reset the last accepted block.
	SetLastAccepted(lastAcceptedHash common.Hash)

	// IsBonus returns true if the block for atomicState is a bonus block
	IsBonus(blockHeight uint64, blockHash common.Hash) bool
}

// atomicBackend implements the AtomicBackend interface using
// the AtomicTrie, AtomicTxRepository, and the VM's shared memory.
type atomicBackend struct {
	codec        codec.Manager
	bonusBlocks  map[uint64]ids.ID   // Map of height to blockID for blocks to skip indexing
	db           *versiondb.Database // Underlying database
	metadataDB   database.Database   // Underlying database containing the atomic trie metadata
	sharedMemory atomic.SharedMemory

	repo       AtomicTxRepository
	atomicTrie AtomicTrie

	lastAcceptedHash common.Hash
	verifiedRoots    map[common.Hash]AtomicState
}

// NewAtomicBackend creates an AtomicBackend from the specified dependencies
func NewAtomicBackend(
	db *versiondb.Database, sharedMemory atomic.SharedMemory,
	bonusBlocks map[uint64]ids.ID, repo AtomicTxRepository,
	lastAcceptedHeight uint64, lastAcceptedHash common.Hash, commitInterval uint64,
) (AtomicBackend, error) {
	atomicTrieDB := prefixdb.New(atomicTrieDBPrefix, db)
	metadataDB := prefixdb.New(atomicTrieMetaDBPrefix, db)
	codec := repo.Codec()

	atomicTrie, err := newAtomicTrie(atomicTrieDB, metadataDB, codec, lastAcceptedHeight, commitInterval)
	if err != nil {
		return nil, err
	}
	atomicBackend := &atomicBackend{
		codec:            codec,
		db:               db,
		metadataDB:       metadataDB,
		sharedMemory:     sharedMemory,
		bonusBlocks:      bonusBlocks,
		repo:             repo,
		atomicTrie:       atomicTrie,
		lastAcceptedHash: lastAcceptedHash,
		verifiedRoots:    make(map[common.Hash]AtomicState),
	}

	// We call ApplyToSharedMemory here to ensure that if the node was shut down in the middle
	// of applying atomic operations from state sync, we finish the operation to ensure we never
	// return an atomic trie that is out of sync with shared memory.
	// In normal operation, the cursor is not set, such that this call will be a no-op.
	if err := atomicBackend.ApplyToSharedMemory(lastAcceptedHeight); err != nil {
		return nil, err
	}
	return atomicBackend, atomicBackend.initialize(lastAcceptedHeight)
}

// initializes the atomic trie using the atomic repository height index.
// Iterating from the last committed height to the last height indexed
// in the atomic repository, making a single commit at the
// most recent height divisible by the commitInterval.
// Subsequent updates to this trie are made using the Index call as blocks are accepted.
// Note: this method assumes no atomic txs are applied at genesis.
func (a *atomicBackend) initialize(lastAcceptedHeight uint64) error {
	start := time.Now()

	// track the last committed height and last committed root
	lastCommittedRoot, lastCommittedHeight := a.atomicTrie.LastCommitted()
	log.Info("initializing atomic trie", "lastCommittedHeight", lastCommittedHeight)

	// iterate by height, from [lastCommittedHeight+1] to [lastAcceptedBlockNumber]
	height := lastCommittedHeight
	iter := a.repo.IterateByHeight(lastCommittedHeight + 1)
	defer iter.Release()

	heightsIndexed := 0
	lastUpdate := time.Now()

	// open the atomic trie at the last committed root
	tr, err := a.atomicTrie.OpenTrie(lastCommittedRoot)
	if err != nil {
		return err
	}

	for iter.Next() {
		// Get the height and transactions for this iteration (from the key and value, respectively)
		// iterate over the transactions, indexing them if the height is < commit height
		// otherwise, add the atomic operations from the transaction to the uncommittedOpsMap
		height = binary.BigEndian.Uint64(iter.Key())
		txs, err := ExtractAtomicTxs(iter.Value(), true, a.codec)
		if err != nil {
			return err
		}

		// combine atomic operations from all transactions at this block height
		combinedOps, err := mergeAtomicOps(txs)
		if err != nil {
			return err
		}

		if _, found := a.bonusBlocks[height]; found {
			// If [height] is a bonus block, do not index the atomic operations into the trie
			continue
		}
		if err := a.atomicTrie.UpdateTrie(tr, height, combinedOps); err != nil {
			return err
		}
		root, nodes, err := tr.Commit(false)
		if err != nil {
			return err
		}
		if err := a.atomicTrie.InsertTrie(nodes, root); err != nil {
			return err
		}
		isCommit, err := a.atomicTrie.AcceptTrie(height, root)
		if err != nil {
			return err
		}
		if isCommit {
			if err := a.db.Commit(); err != nil {
				return err
			}
		}

		heightsIndexed++
		if time.Since(lastUpdate) > progressLogFrequency {
			log.Info("imported entries into atomic trie", "heightsIndexed", heightsIndexed)
			lastUpdate = time.Now()
		}
	}
	if err := iter.Error(); err != nil {
		return err
	}

	// check if there are accepted blocks after the last block with accepted atomic txs.
	if lastAcceptedHeight > height {
		lastAcceptedRoot := a.atomicTrie.LastAcceptedRoot()
		if err := a.atomicTrie.InsertTrie(nil, lastAcceptedRoot); err != nil {
			return err
		}
		if _, err := a.atomicTrie.AcceptTrie(lastAcceptedHeight, lastAcceptedRoot); err != nil {
			return err
		}
	}

	lastCommittedRoot, lastCommittedHeight = a.atomicTrie.LastCommitted()
	log.Info(
		"finished initializing atomic trie",
		"lastAcceptedHeight", lastAcceptedHeight,
		"lastAcceptedAtomicRoot", a.atomicTrie.LastAcceptedRoot(),
		"heightsIndexed", heightsIndexed,
		"lastCommittedRoot", lastCommittedRoot,
		"lastCommittedHeight", lastCommittedHeight,
		"time", time.Since(start),
	)
	return nil
}

// ApplyToSharedMemory applies the atomic operations that have been indexed into the trie
// but not yet applied to shared memory for heights less than or equal to [lastAcceptedBlock].
// This executes operations in the range [cursorHeight+1, lastAcceptedBlock].
// The cursor is initially set by  MarkApplyToSharedMemoryCursor to signal to the atomic trie
// the range of operations that were added to the trie without being executed on shared memory.
func (a *atomicBackend) ApplyToSharedMemory(lastAcceptedBlock uint64) error {
	sharedMemoryCursor, err := a.metadataDB.Get(appliedSharedMemoryCursorKey)
	if err == database.ErrNotFound {
		return nil
	} else if err != nil {
		return err
	}

	lastCommittedRoot, _ := a.atomicTrie.LastCommitted()
	log.Info("applying atomic operations to shared memory", "root", lastCommittedRoot, "lastAcceptedBlock", lastAcceptedBlock, "startHeight", binary.BigEndian.Uint64(sharedMemoryCursor[:wrappers.LongLen]))

	it, err := a.atomicTrie.Iterator(lastCommittedRoot, sharedMemoryCursor)
	if err != nil {
		return err
	}

	var (
		lastUpdate                            = time.Now()
		putRequests, removeRequests           = 0, 0
		totalPutRequests, totalRemoveRequests = 0, 0
	)

	// value of sharedMemoryCursor is either a uint64 signifying the
	// height iteration should begin at or is a uint64+blockchainID
	// specifying the last atomic operation that was applied to shared memory.
	// To avoid applying the same operation twice, we call [it.Next()] in the
	// latter case.
	if len(sharedMemoryCursor) > wrappers.LongLen {
		it.Next()
	}

	batchOps := make(map[ids.ID]*atomic.Requests)
	for it.Next() {
		height := it.BlockNumber()
		atomicOps := it.AtomicOps()

		if height > lastAcceptedBlock {
			log.Warn("Found height above last accepted block while applying operations to shared memory", "height", height, "lastAcceptedBlock", lastAcceptedBlock)
			break
		}

		putRequests += len(atomicOps.PutRequests)
		removeRequests += len(atomicOps.RemoveRequests)
		totalPutRequests += len(atomicOps.PutRequests)
		totalRemoveRequests += len(atomicOps.RemoveRequests)
		if time.Since(lastUpdate) > progressLogFrequency {
			log.Info("atomic trie iteration", "height", height, "puts", totalPutRequests, "removes", totalRemoveRequests)
			lastUpdate = time.Now()
		}
		mergeAtomicOpsToMap(batchOps, it.BlockchainID(), atomicOps)

		if putRequests+removeRequests > sharedMemoryApplyBatchSize {
			// Update the cursor to the key of the atomic operation being executed on shared memory.
			// If the node shuts down in the middle of this function call, ApplyToSharedMemory will
			// resume operation starting at the key immediately following [it.Key()].
			if err = a.metadataDB.Put(appliedSharedMemoryCursorKey, it.Key()); err != nil {
				return err
			}
			batch, err := a.db.CommitBatch()
			if err != nil {
				return err
			}
			// calling [sharedMemory.Apply] updates the last applied pointer atomically with the shared memory operation.
			if err = a.sharedMemory.Apply(batchOps, batch); err != nil {
				return err
			}
			putRequests, removeRequests = 0, 0
			batchOps = make(map[ids.ID]*atomic.Requests)
		}
	}
	if err := it.Error(); err != nil {
		return err
	}

	if err = a.metadataDB.Delete(appliedSharedMemoryCursorKey); err != nil {
		return err
	}
	batch, err := a.db.CommitBatch()
	if err != nil {
		return err
	}
	if err = a.sharedMemory.Apply(batchOps, batch); err != nil {
		return err
	}
	log.Info("finished applying atomic operations", "puts", totalPutRequests, "removes", totalRemoveRequests)
	return nil
}

// MarkApplyToSharedMemoryCursor marks the atomic trie as containing atomic ops that
// have not been executed on shared memory starting at [previousLastAcceptedHeight+1].
// This is used when state sync syncs the atomic trie, such that the atomic operations
// from [previousLastAcceptedHeight+1] to the [lastAcceptedHeight] set by state sync
// will not have been executed on shared memory.
func (a *atomicBackend) MarkApplyToSharedMemoryCursor(previousLastAcceptedHeight uint64) error {
	// Set the cursor to [previousLastAcceptedHeight+1] so that we begin the iteration at the
	// first item that has not been applied to shared memory.
	return database.PutUInt64(a.metadataDB, appliedSharedMemoryCursorKey, previousLastAcceptedHeight+1)
}

// Syncer creates and returns a new Syncer object that can be used to sync the
// state of the atomic trie from peers
func (a *atomicBackend) Syncer(client syncclient.LeafClient, targetRoot common.Hash, targetHeight uint64) (Syncer, error) {
	return newAtomicSyncer(client, a, targetRoot, targetHeight)
}

func (a *atomicBackend) GetVerifiedAtomicState(blockHash common.Hash) (AtomicState, error) {
	if state, ok := a.verifiedRoots[blockHash]; ok {
		return state, nil
	}
	return nil, fmt.Errorf("cannot access atomic state for block %s", blockHash)
}

// getAtomicRootAt returns the atomic trie root for a block that is either:
// - the last accepted block
// - a block that has been verified but not accepted or rejected yet.
// If [blockHash] is neither of the above, an error is returned.
func (a *atomicBackend) getAtomicRootAt(blockHash common.Hash) (common.Hash, error) {
	// TODO: we can implement this in a few ways.
	if blockHash == a.lastAcceptedHash {
		return a.atomicTrie.LastAcceptedRoot(), nil
	}
	state, err := a.GetVerifiedAtomicState(blockHash)
	if err != nil {
		return common.Hash{}, err
	}
	return state.Root(), nil
}

// SetLastAccepted is used after state-sync to update the last accepted block hash.
func (a *atomicBackend) SetLastAccepted(lastAcceptedHash common.Hash) {
	a.lastAcceptedHash = lastAcceptedHash
}

// InsertTxs calculates the root of the atomic trie that would
// result from applying [txs] to the atomic trie, starting at the state
// corresponding to previously verified block [parentHash].
// If [blockHash] is provided, the modified atomic trie is pinned in memory
// and it's the caller's responsibility to call either Accept or Reject on
// the AtomicState which can be retreived from GetVerifiedAtomicState to commit the
// changes or abort them and free memory.
func (a *atomicBackend) InsertTxs(blockHash common.Hash, blockHeight uint64, parentHash common.Hash, txs []*Tx) (common.Hash, error) {
	// access the atomic trie at the parent block
	parentRoot, err := a.getAtomicRootAt(parentHash)
	if err != nil {
		return common.Hash{}, err
	}
	tr, err := a.atomicTrie.OpenTrie(parentRoot)
	if err != nil {
		return common.Hash{}, err
	}

	// update the atomic trie
	atomicOps, err := mergeAtomicOps(txs)
	if err != nil {
		return common.Hash{}, err
	}
	if err := a.atomicTrie.UpdateTrie(tr, blockHeight, atomicOps); err != nil {
		return common.Hash{}, err
	}

	// If block hash is not provided, we do not pin the atomic state in memory and can return early
	if blockHash == (common.Hash{}) {
		return tr.Hash(), nil
	}

	// get the new root and pin the atomic trie changes in memory.
	root, nodes, err := tr.Commit(false)
	if err != nil {
		return common.Hash{}, err
	}
	if err := a.atomicTrie.InsertTrie(nodes, root); err != nil {
		return common.Hash{}, err
	}
	// track this block so further blocks can be inserted on top
	// of this block
	a.verifiedRoots[blockHash] = &atomicState{
		backend:     a,
		blockHash:   blockHash,
		blockHeight: blockHeight,
		txs:         txs,
		atomicOps:   atomicOps,
		atomicRoot:  root,
	}
	return root, nil
}

// IsBonus returns true if the block for atomicState is a bonus block
func (a *atomicBackend) IsBonus(blockHeight uint64, blockHash common.Hash) bool {
	if bonusID, found := a.bonusBlocks[blockHeight]; found {
		return bonusID == ids.ID(blockHash)
	}
	return false
}

func (a *atomicBackend) AtomicTrie() AtomicTrie {
	return a.atomicTrie
}
