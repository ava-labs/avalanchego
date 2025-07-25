// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"errors"
)

// ErrWaitBeforeStart is returned when Wait() is called before Start().
var ErrWaitBeforeStart = errors.New("Wait() called before Start() - call Start() first")

// Syncer is the common interface for all sync operations.
// This provides a unified interface for atomic state sync and state trie sync.
type Syncer interface {
	// Start begins the sync operation.
	// The sync will respect context cancellation.
	Start(ctx context.Context) error

	// Wait blocks until the sync operation completes or fails.
	// Returns the final error (nil if successful).
	// The sync will respect context cancellation.
	Wait(ctx context.Context) error
}
