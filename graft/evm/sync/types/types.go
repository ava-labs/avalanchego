// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"context"

	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/graft/evm/message"
)

// Syncer is the common interface for all sync operations.
// This provides a unified interface for atomic state sync and state trie sync.
type Syncer interface {
	// Sync completes the full sync operation, returning any errors encountered.
	// The sync will respect context cancellation.
	Sync(ctx context.Context) error

	// Name returns a human-readable name for this syncer implementation.
	Name() string

	// ID returns a stable, machine-oriented identifier (e.g., "state_block_sync", "state_code_sync",
	// "state_evm_state_sync", "state_atomic_sync"). Implementations should ensure this is unique and
	// stable across renames for logging/metrics/deduplication.
	ID() string
}

// Finalizer provides a mechanism to perform cleanup operations after a sync operation.
// This is useful for handling inflight requests, flushing to disk, or other cleanup tasks.
type Finalizer interface {
	// Finalize performs any necessary cleanup operations.
	Finalize() error
}

// LeafClient is the interface for fetching leaves from the network.
// This is defined here to avoid circular dependencies with the leaf package.
type LeafClient interface {
	// GetLeafs synchronously sends the given request, returning a parsed LeafsResponse or error.
	// Note: this verifies the response including the range proofs.
	GetLeafs(ctx context.Context, request message.LeafsRequest) (message.LeafsResponse, error)
}

// Extender is an interface that allows for extending the state sync process.
type Extender interface {
	// CreateSyncer creates a syncer instance for the given client, database, and summary.
	CreateSyncer(client LeafClient, verDB *versiondb.Database, summary message.Syncable) (Syncer, error)

	// OnFinishBeforeCommit is called before committing the sync results.
	OnFinishBeforeCommit(lastAcceptedHeight uint64, summary message.Syncable) error

	// OnFinishAfterCommit is called after committing the sync results.
	OnFinishAfterCommit(summaryHeight uint64) error
}
