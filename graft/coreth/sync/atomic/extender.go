// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package atomic

import (
	"context"
	"fmt"

	"github.com/ava-labs/coreth/plugin/evm/atomic/state"

	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/coreth/plugin/evm/message"
	syncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ava-labs/coreth/sync/vm"
)

var _ vm.Extender = (*Extender)(nil)

// Extender is the sync extender for the atomic VM.
type Extender struct {
	backend     *state.AtomicBackend
	trie        *state.AtomicTrie
	requestSize uint16 // maximum number of leaves to sync in a single request
}

// Initialize initializes the sync extender with the backend and trie and request size.
func (a *Extender) Initialize(backend *state.AtomicBackend, trie *state.AtomicTrie, requestSize uint16) {
	a.backend = backend
	a.trie = trie
	a.requestSize = requestSize
}

// Sync syncs the atomic summary with the given client and verDB.
func (a *Extender) Sync(ctx context.Context, client syncclient.LeafClient, verDB *versiondb.Database, summary message.Syncable) error {
	atomicSummary, ok := summary.(*Summary)
	if !ok {
		return fmt.Errorf("expected *Summary, got %T", summary)
	}
	log.Info("atomic sync starting", "summary", atomicSummary)

	// Use default number of workers for atomic syncing.
	// This can be made configurable in the future.
	config := Config{
		Client:       client,
		Database:     verDB,
		AtomicTrie:   a.trie,
		TargetRoot:   atomicSummary.AtomicRoot,
		TargetHeight: atomicSummary.BlockNumber,
		RequestSize:  a.requestSize,
		NumWorkers:   defaultNumWorkers,
	}
	syncer, err := newSyncer(&config)
	if err != nil {
		return fmt.Errorf("failed to create atomic syncer: %w", err)
	}
	if err := syncer.Start(ctx); err != nil {
		return fmt.Errorf("failed to start atomic syncer: %w", err)
	}

	err = syncer.Wait(ctx)
	log.Info("atomic sync finished", "summary", atomicSummary, "err", err)

	return err
}

// OnFinishBeforeCommit implements the sync.Extender interface by marking the previously last accepted block for the shared memory cursor.
func (a *Extender) OnFinishBeforeCommit(lastAcceptedHeight uint64, Summary message.Syncable) error {
	// Mark the previously last accepted block for the shared memory cursor, so that we will execute shared
	// memory operations from the previously last accepted block when ApplyToSharedMemory
	// is called.
	if err := a.backend.MarkApplyToSharedMemoryCursor(lastAcceptedHeight); err != nil {
		return fmt.Errorf("failed to mark apply to shared memory cursor before commit: %w", err)
	}
	a.backend.SetLastAccepted(Summary.GetBlockHash())
	return nil
}

// OnFinishAfterCommit implements the sync.Extender interface by applying the atomic trie to the shared memory.
func (a *Extender) OnFinishAfterCommit(summaryHeight uint64) error {
	// the chain state is already restored, and, from this point on,
	// the block synced to is the accepted block. The last operation
	// is updating shared memory with the atomic trie.
	// ApplyToSharedMemory does this, and, even if the VM is stopped
	// (gracefully or ungracefully), since MarkApplyToSharedMemoryCursor
	// is called, VM will resume ApplyToSharedMemory on Initialize.
	if err := a.backend.ApplyToSharedMemory(summaryHeight); err != nil {
		return fmt.Errorf("failed to apply atomic trie to shared memory after commit: %w", err)
	}
	return nil
}
