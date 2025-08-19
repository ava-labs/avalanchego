// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/coreth/plugin/evm/atomic"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
)

// atomicState implements the AtomicState interface using
// a pointer to the atomicBackend.
type atomicState struct {
	backend     *AtomicBackend
	blockHash   common.Hash
	blockHeight uint64
	txs         []*atomic.Tx
	atomicOps   map[ids.ID]*avalancheatomic.Requests
	atomicRoot  common.Hash
}

func (a *atomicState) Root() common.Hash {
	return a.atomicRoot
}

// Accept writes the atomic operations to the database and
// updates the last accepted block in the atomic backend.
// It also commits the `commitBatch` to the shared memory.
func (a *atomicState) Accept(commitBatch database.Batch) error {
	isBonus := a.backend.IsBonus(a.blockHeight, a.blockHash)
	// Update the atomic tx repository. Note it is necessary to invoke
	// the correct method taking bonus blocks into consideration.
	if isBonus {
		if err := a.backend.repo.WriteBonus(a.blockHeight, a.txs); err != nil {
			return err
		}
	} else {
		if err := a.backend.repo.Write(a.blockHeight, a.txs); err != nil {
			return err
		}
	}

	// Accept the root of this atomic trie (will be persisted if at a commit interval)
	if _, err := a.backend.atomicTrie.AcceptTrie(a.blockHeight, a.atomicRoot); err != nil {
		return err
	}
	// Update the last accepted block to this block and remove it from
	// the map tracking undecided blocks.
	a.backend.lastAcceptedHash = a.blockHash
	delete(a.backend.verifiedRoots, a.blockHash)

	// get changes from the atomic trie and repository in a batch
	// to be committed atomically with [commitBatch] and shared memory.
	atomicChangesBatch, err := a.backend.repo.db.CommitBatch()
	if err != nil {
		return fmt.Errorf("could not create commit batch in atomicState accept: %w", err)
	}

	// If this is a bonus block, write [commitBatch] without applying atomic ops
	// to shared memory.
	if isBonus {
		log.Info("skipping atomic tx acceptance on bonus block", "block", a.blockHash)
		return avalancheatomic.WriteAll(commitBatch, atomicChangesBatch)
	}

	// Otherwise, atomically commit pending changes in the version db with
	// atomic ops to shared memory.
	return a.backend.sharedMemory.Apply(a.atomicOps, commitBatch, atomicChangesBatch)
}

// Reject frees memory associated with the state change.
func (a *atomicState) Reject() error {
	// Remove the block from the map of undecided blocks.
	delete(a.backend.verifiedRoots, a.blockHash)
	// Unpin the rejected atomic trie root from memory.
	return a.backend.atomicTrie.RejectTrie(a.atomicRoot)
}
