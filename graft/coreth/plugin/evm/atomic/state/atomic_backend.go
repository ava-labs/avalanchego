// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
)

const (
	sharedMemoryApplyBatchSize = 10_000 // specifies the number of atomic operations to batch progress updates
	progressLogFrequency       = 30 * time.Second
)

// AtomicBackend provides an interface to the atomic trie and shared memory.
// the AtomicTrie, AtomicRepository, and the VM's shared memory.
type AtomicBackend struct {
	codec        codec.Manager
	bonusBlocks  map[uint64]ids.ID // Map of height to blockID for blocks to skip indexing
	sharedMemory avalancheatomic.SharedMemory

	repo       *AtomicRepository
	atomicTrie *AtomicTrie

	lastAcceptedHash common.Hash
	verifiedRoots    map[common.Hash]*atomicState
}

// NewAtomicBackend creates an AtomicBackend from the specified dependencies
func NewAtomicBackend(
	sharedMemory avalancheatomic.SharedMemory,
	bonusBlocks map[uint64]ids.ID, repo *AtomicRepository,
	lastAcceptedHeight uint64, lastAcceptedHash common.Hash, commitInterval uint64,
) (*AtomicBackend, error) {
	codec := repo.codec

	atomicTrie, err := newAtomicTrie(repo.atomicTrieDB, repo.metadataDB, codec, lastAcceptedHeight, commitInterval)
	if err != nil {
		return nil, err
	}
	atomicBackend := &AtomicBackend{
		codec:            codec,
		sharedMemory:     sharedMemory,
		bonusBlocks:      bonusBlocks,
		repo:             repo,
		atomicTrie:       atomicTrie,
		lastAcceptedHash: lastAcceptedHash,
		verifiedRoots:    make(map[common.Hash]*atomicState),
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
func (a *AtomicBackend) initialize(lastAcceptedHeight uint64) error {
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
		txs, err := atomic.ExtractAtomicTxs(iter.Value(), true, a.codec)
		if err != nil {
			return err
		}

		// combine atomic operations from all transactions at this block height
		combinedOps, err := mergeAtomicOps(txs)
		if err != nil {
			return err
		}

		// Note: The atomic trie canonically contains the duplicate operations
		// from any bonus blocks.
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
			if err := a.repo.db.Commit(); err != nil {
				return err
			}
		}
		// Trie must be re-opened after committing (not safe for re-use after commit)
		tr, err = a.atomicTrie.OpenTrie(root)
		if err != nil {
			return err
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
func (a *AtomicBackend) ApplyToSharedMemory(lastAcceptedBlock uint64) error {
	sharedMemoryCursor, err := a.repo.metadataDB.Get(appliedSharedMemoryCursorKey)
	if err == database.ErrNotFound {
		return nil
	} else if err != nil {
		return err
	}

	lastHeight := binary.BigEndian.Uint64(sharedMemoryCursor[:wrappers.LongLen])

	lastCommittedRoot, _ := a.atomicTrie.LastCommitted()
	log.Info("applying atomic operations to shared memory", "root", lastCommittedRoot, "lastAcceptedBlock", lastAcceptedBlock, "startHeight", lastHeight)

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
	var lastBlockchainID ids.ID
	if len(sharedMemoryCursor) > wrappers.LongLen {
		lastBlockchainID, err = ids.ToID(sharedMemoryCursor[wrappers.LongLen:])
		if err != nil {
			return err
		}

		it.Next()
	}

	batchOps := make(map[ids.ID]*avalancheatomic.Requests)
	for it.Next() {
		height := it.BlockNumber()
		if height > lastAcceptedBlock {
			log.Warn("Found height above last accepted block while applying operations to shared memory", "height", height, "lastAcceptedBlock", lastAcceptedBlock)
			break
		}

		// If [height] is a bonus block, do not apply the atomic operations to shared memory
		if _, found := a.bonusBlocks[height]; found {
			log.Debug(
				"skipping bonus block in applying atomic ops from atomic trie to shared memory",
				"height", height,
			)
			continue
		}

		atomicOps := it.AtomicOps()
		putRequests += len(atomicOps.PutRequests)
		removeRequests += len(atomicOps.RemoveRequests)
		totalPutRequests += len(atomicOps.PutRequests)
		totalRemoveRequests += len(atomicOps.RemoveRequests)
		if time.Since(lastUpdate) > progressLogFrequency {
			log.Info("atomic trie iteration", "height", height, "puts", totalPutRequests, "removes", totalRemoveRequests)
			lastUpdate = time.Now()
		}

		blockchainID := it.BlockchainID()
		mergeAtomicOpsToMap(batchOps, blockchainID, atomicOps)

		if putRequests+removeRequests > sharedMemoryApplyBatchSize {
			// Update the cursor to the key of the atomic operation being executed on shared memory.
			// If the node shuts down in the middle of this function call, ApplyToSharedMemory will
			// resume operation starting at the key immediately following [it.Key()].
			if err = a.repo.metadataDB.Put(appliedSharedMemoryCursorKey, it.Key()); err != nil {
				return err
			}
			batch, err := a.repo.db.CommitBatch()
			if err != nil {
				return err
			}
			// calling [sharedMemory.Apply] updates the last applied pointer atomically with the shared memory operation.
			if err = a.sharedMemory.Apply(batchOps, batch); err != nil {
				return fmt.Errorf("failed committing shared memory operations between %d:%s and %d:%s with: %w",
					lastHeight, lastBlockchainID,
					height, blockchainID,
					err,
				)
			}
			lastHeight = height
			lastBlockchainID = blockchainID
			putRequests, removeRequests = 0, 0
			batchOps = make(map[ids.ID]*avalancheatomic.Requests)
		}
	}
	if err := it.Error(); err != nil {
		return err
	}

	if err = a.repo.metadataDB.Delete(appliedSharedMemoryCursorKey); err != nil {
		return err
	}
	batch, err := a.repo.db.CommitBatch()
	if err != nil {
		return err
	}
	if err = a.sharedMemory.Apply(batchOps, batch); err != nil {
		return fmt.Errorf("failed committing shared memory operations between %d:%s and %d with: %w",
			lastHeight, lastBlockchainID,
			lastAcceptedBlock,
			err,
		)
	}
	log.Info("finished applying atomic operations", "puts", totalPutRequests, "removes", totalRemoveRequests)
	return nil
}

// MarkApplyToSharedMemoryCursor marks the atomic trie as containing atomic ops that
// have not been executed on shared memory starting at [previousLastAcceptedHeight+1].
// This is used when state sync syncs the atomic trie, such that the atomic operations
// from [previousLastAcceptedHeight+1] to the [lastAcceptedHeight] set by state sync
// will not have been executed on shared memory.
func (a *AtomicBackend) MarkApplyToSharedMemoryCursor(previousLastAcceptedHeight uint64) error {
	// Set the cursor to [previousLastAcceptedHeight+1] so that we begin the iteration at the
	// first item that has not been applied to shared memory.
	return database.PutUInt64(a.repo.metadataDB, appliedSharedMemoryCursorKey, previousLastAcceptedHeight+1)
}

func (a *AtomicBackend) GetVerifiedAtomicState(blockHash common.Hash) (*atomicState, error) {
	if state, ok := a.verifiedRoots[blockHash]; ok {
		return state, nil
	}
	return nil, fmt.Errorf("cannot access atomic state for block %s", blockHash)
}

// getAtomicRootAt returns the atomic trie root for a block that is either:
// - the last accepted block
// - a block that has been verified but not accepted or rejected yet.
// If [blockHash] is neither of the above, an error is returned.
func (a *AtomicBackend) getAtomicRootAt(blockHash common.Hash) (common.Hash, error) {
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
func (a *AtomicBackend) SetLastAccepted(lastAcceptedHash common.Hash) {
	a.lastAcceptedHash = lastAcceptedHash
}

// InsertTxs calculates the root of the atomic trie that would
// result from applying [txs] to the atomic trie, starting at the state
// corresponding to previously verified block [parentHash].
// If [blockHash] is provided, the modified atomic trie is pinned in memory
// and it's the caller's responsibility to call either Accept or Reject on
// the AtomicState which can be retreived from GetVerifiedAtomicState to commit the
// changes or abort them and free memory.
func (a *AtomicBackend) InsertTxs(blockHash common.Hash, blockHeight uint64, parentHash common.Hash, txs []*atomic.Tx) (common.Hash, error) {
	// access the atomic trie at the parent block
	parentRoot, err := a.getAtomicRootAt(parentHash)
	if err != nil {
		return common.Hash{}, err
	}
	tr, err := a.atomicTrie.OpenTrie(parentRoot)
	if err != nil {
		return common.Hash{}, err
	}

	// Insert the operations into the atomic trie
	//
	// Note: The atomic trie canonically contains the duplicate operations from
	// any bonus blocks.
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
func (a *AtomicBackend) IsBonus(blockHeight uint64, blockHash common.Hash) bool {
	if bonusID, found := a.bonusBlocks[blockHeight]; found {
		return bonusID == ids.ID(blockHash)
	}
	return false
}

func (a *AtomicBackend) AtomicTrie() *AtomicTrie {
	return a.atomicTrie
}

// mergeAtomicOps merges atomic requests represented by [txs]
// to the [output] map, depending on whether [chainID] is present in the map.
func mergeAtomicOps(txs []*atomic.Tx) (map[ids.ID]*avalancheatomic.Requests, error) {
	if len(txs) > 1 {
		// txs should be stored in order of txID to ensure consistency
		// with txs initialized from the txID index.
		copyTxs := make([]*atomic.Tx, len(txs))
		copy(copyTxs, txs)
		utils.Sort(copyTxs)
		txs = copyTxs
	}
	output := make(map[ids.ID]*avalancheatomic.Requests)
	for _, tx := range txs {
		chainID, txRequests, err := tx.UnsignedAtomicTx.AtomicOps()
		if err != nil {
			return nil, err
		}
		mergeAtomicOpsToMap(output, chainID, txRequests)
	}
	return output, nil
}

// mergeAtomicOps merges atomic ops for [chainID] represented by [requests]
// to the [output] map provided.
func mergeAtomicOpsToMap(output map[ids.ID]*avalancheatomic.Requests, chainID ids.ID, requests *avalancheatomic.Requests) {
	if request, exists := output[chainID]; exists {
		request.PutRequests = append(request.PutRequests, requests.PutRequests...)
		request.RemoveRequests = append(request.RemoveRequests, requests.RemoveRequests...)
	} else {
		output[chainID] = requests
	}
}

// AddBonusBlock adds a bonus block to the atomic backend
func (a *AtomicBackend) AddBonusBlock(height uint64, blockID ids.ID) {
	if a.bonusBlocks == nil {
		a.bonusBlocks = make(map[uint64]ids.ID)
	}
	a.bonusBlocks[height] = blockID
}
