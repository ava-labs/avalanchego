// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"context"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/graft/evm/message"
)

// Syncer is the common interface for all sync operations.
type Syncer interface {
	// Sync runs the full sync, respecting context cancellation.
	Sync(ctx context.Context) error
	// UpdateTarget updates the sync target mid-sync. Static syncers may no-op.
	UpdateTarget(newTarget message.Syncable) error
	// Name returns a human-readable name for logging.
	Name() string
	// ID returns a stable machine-oriented identifier for metrics and dedup.
	ID() string
}

// Finalizer flushes in-progress work (inflight requests, disk writes, etc.).
type Finalizer interface {
	Finalize() error
}

// PivotSession represents one sync session inside a DynamicSyncer. When the
// target changes, the current session is cancelled and Rebuild creates a
// fresh session for the new target.
type PivotSession interface {
	// Run syncs to completion or until ctx is cancelled.
	Run(ctx context.Context) error
	// Rebuild cleans up the current session and returns a new one for the
	// given root and height.
	Rebuild(newRoot common.Hash, newHeight uint64) (PivotSession, error)
	// ShouldPivot reports whether newRoot requires restarting. Returning
	// false lets the loop bump the height without restarting.
	ShouldPivot(newRoot common.Hash) bool
	// OnSessionComplete is called once when sync finishes successfully.
	OnSessionComplete() error
}

// CodeRequestQueue enqueues code hashes for the code syncer to fetch.
type CodeRequestQueue interface {
	AddCode(context.Context, []common.Hash) error
	// Finalize closes the queue, signalling the code syncer to exit.
	Finalize() error
}

// LeafClient fetches leaves from the network. Responses include verified
// range proofs. Defined here to avoid circular deps with the leaf package.
type LeafClient interface {
	GetLeafs(ctx context.Context, request message.LeafsRequest) (message.LeafsResponse, error)
}

// Extender hooks into the state sync lifecycle for VM-specific work
// (e.g., atomic trie sync in coreth).
type Extender interface {
	CreateSyncer(client LeafClient, verDB *versiondb.Database, summary message.Syncable) (Syncer, error)
	OnFinishBeforeCommit(lastAcceptedHeight uint64, summary message.Syncable) error
	OnFinishAfterCommit(summaryHeight uint64) error
}
