// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/state"
	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
)

// Extender is the sync extender for the atomic VM.
type Extender struct {
	backend     *state.AtomicBackend
	trie        *state.AtomicTrie
	requestSize uint16 // maximum number of leaves to sync in a single request
}

// Initialize initializes the sync extender with the backend and trie and request size.
func (e *Extender) Initialize(backend *state.AtomicBackend, trie *state.AtomicTrie, requestSize uint16) {
	e.backend = backend
	e.trie = trie
	e.requestSize = requestSize
}

// CreateSyncer creates the atomic syncer with the given client and verDB.
func (e *Extender) CreateSyncer(client types.LeafClient, verDB *versiondb.Database, summary message.Syncable) (types.Syncer, error) {
	atomicSummary, ok := summary.(*Summary)
	if !ok {
		return nil, fmt.Errorf("atomic sync extender: expected *Summary, got %T", summary)
	}

	syncer, err := NewSyncer(
		client,
		verDB,
		e.trie,
		atomicSummary.AtomicRoot,
		atomicSummary.BlockNumber,
		WithRequestSize(e.requestSize),
	)
	if err != nil {
		return nil, fmt.Errorf("atomic.NewSyncer failed: %w", err)
	}
	return syncer, nil
}

// OnFinishBeforeCommit implements the sync.Extender interface by marking the previously last accepted block for the shared memory cursor.
func (e *Extender) OnFinishBeforeCommit(lastAcceptedHeight uint64, summary message.Syncable) error {
	// Mark the previously last accepted block for the shared memory cursor, so that we will execute shared
	// memory operations from the previously last accepted block when ApplyToSharedMemory
	// is called.
	if err := e.backend.MarkApplyToSharedMemoryCursor(lastAcceptedHeight); err != nil {
		return fmt.Errorf("failed to mark apply to shared memory cursor before commit: %w", err)
	}
	e.backend.SetLastAccepted(summary.GetBlockHash())
	return nil
}

// OnFinishAfterCommit implements the sync.Extender interface by applying the atomic trie to the shared memory.
func (e *Extender) OnFinishAfterCommit(summaryHeight uint64) error {
	// the chain state is already restored, and, from this point on,
	// the block synced to is the accepted block. The last operation
	// is updating shared memory with the atomic trie.
	// ApplyToSharedMemory does this, and, even if the VM is stopped
	// (gracefully or ungracefully), since MarkApplyToSharedMemoryCursor
	// is called, VM will resume ApplyToSharedMemory on Initialize.
	if err := e.backend.ApplyToSharedMemory(summaryHeight); err != nil {
		return fmt.Errorf("failed to apply atomic trie to shared memory after commit: %w", err)
	}
	return nil
}
