// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"context"

	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/version"
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

// SummaryProvider is an interface for providing state summaries.
type SummaryProvider interface {
	StateSummaryAtBlock(ethBlock *types.Block) (block.StateSummary, error)
}



// SyncedNetworkClient abstracts the Avalanche network client
type SyncedNetworkClient interface {
	// SendSyncedAppRequest sends a request to a specific node
	SendSyncedAppRequest(ctx context.Context, nodeID ids.NodeID, request []byte) ([]byte, error)
	// SendSyncedAppRequestAny sends a request to any available node
	SendSyncedAppRequestAny(ctx context.Context, version *version.Application, request []byte) ([]byte, ids.NodeID, error)
	// TrackBandwidth tracks bandwidth usage for a node
	TrackBandwidth(nodeID ids.NodeID, bandwidth float64)
}
