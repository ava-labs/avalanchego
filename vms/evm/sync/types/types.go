// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import "context"

// Syncer is the interface every sync operation implements, independent of its
// mechanism or storage layer.
type Syncer interface {
	// Sync runs the full sync to completion, honoring context cancellation.
	Sync(ctx context.Context) error

	// Name returns a human-readable name for logging.
	Name() string

	// ID returns a stable machine identifier (e.g. "state_evm_state_sync") for
	// logging, metrics, and deduplication. It must stay unique and unchanged
	// across renames.
	ID() string
}

// Finalizer performs cleanup after a sync, such as flushing in-flight writes to
// disk. Implemented by syncers that persist progress.
type Finalizer interface {
	// Finalize runs the cleanup.
	Finalize() error
}
