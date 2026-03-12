// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/graft/evm/core/state/snapshot"
	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/sync/client"
	"github.com/ava-labs/avalanchego/graft/evm/sync/code"
	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
)

var _ types.Syncer = (*HashDBDynamicSyncer)(nil)

// HashDBDynamicSyncer wraps a [HashDBSyncer] and adds pivot-anytime support.
// On each pivot, the inner [HashDBSyncer] is discarded and a fresh one is created
// for the new root, keeping the static sync engine completely unaware of pivots.
type HashDBDynamicSyncer struct {
	inner *HashDBSyncer

	// Retained for rebuilding the inner syncer on pivot.
	syncClient       client.Client
	db               ethdb.Database
	leafsRequestSize uint16
	leafsRequestType message.LeafsRequestType
	opts             []HashDBSyncerOption

	// Session-aware code queue.
	codeQueue *code.SessionedQueue

	// Target tracking (protected by targetMu).
	targetMu      sync.Mutex
	desiredRoot   common.Hash
	desiredHeight uint64
	sessionCancel context.CancelCauseFunc
}

// NewHashDBDynamicSyncer creates a state syncer that supports pivoting to a new root
// mid-sync via UpdateTarget. It wraps a static HashDBSyncer and rebuilds it on
// each pivot.
func NewHashDBDynamicSyncer(syncClient client.Client, db ethdb.Database, root common.Hash, codeQueue *code.SessionedQueue, leafsRequestSize uint16, leafsRequestType message.LeafsRequestType, opts ...HashDBSyncerOption) (types.Syncer, error) {
	inner, err := NewHashDBSyncer(syncClient, db, root, codeQueue, leafsRequestSize, leafsRequestType, opts...)
	if err != nil {
		return nil, err
	}

	return &HashDBDynamicSyncer{
		inner:            inner,
		syncClient:       syncClient,
		db:               db,
		leafsRequestSize: leafsRequestSize,
		leafsRequestType: leafsRequestType,
		opts:             opts,
		codeQueue:        codeQueue,
		desiredRoot:      root,
	}, nil
}

func (*HashDBDynamicSyncer) Name() string { return "HashDB EVM State Syncer (dynamic)" }
func (*HashDBDynamicSyncer) ID() string   { return StateSyncerID }

// Finalize delegates to the current inner syncer's Finalize.
func (d *HashDBDynamicSyncer) Finalize() error {
	return d.inner.Finalize()
}

// Sync runs the session-restart loop. Each iteration syncs a single root via
// the inner HashDBSyncer. If UpdateTarget triggers a pivot, the current session
// is cancelled, a fresh inner syncer is built, and syncing restarts.
func (d *HashDBDynamicSyncer) Sync(ctx context.Context) error {
	if _, err := d.codeQueue.Start(d.inner.root); err != nil {
		return err
	}

	for {
		sessionCtx, sessionCancel := context.WithCancelCause(ctx)
		d.setSessionCancel(sessionCancel)

		err := d.inner.Sync(sessionCtx)

		d.setSessionCancel(nil)
		sessionCancel(nil)

		if err == nil {
			return d.codeQueue.Finalize()
		}

		if !errors.Is(context.Cause(sessionCtx), errPivotRequested) {
			return err
		}

		newRoot := d.getDesiredRoot()

		log.Info("state syncer pivoting to new root", "oldRoot", d.inner.root, "newRoot", newRoot)

		if _, _, err := d.codeQueue.PivotTo(newRoot); err != nil {
			return fmt.Errorf("failed to pivot code queue: %w", err)
		}

		// Best-effort flush of in-progress batches to preserve partial progress.
		if err := d.inner.Finalize(); err != nil {
			log.Error("failed to flush in-progress batches during pivot", "err", err)
		}

		// Wipe snapshot data from the previous session so stale entries
		// don't corrupt the new root's stack trie hash.
		<-snapshot.WipeSnapshot(d.db, false)

		newInner, err := NewHashDBSyncer(d.syncClient, d.db, newRoot, d.codeQueue, d.leafsRequestSize, d.leafsRequestType, d.opts...)
		if err != nil {
			return fmt.Errorf("failed to create syncer for root %s: %w", newRoot, err)
		}
		d.inner = newInner
	}
}

// UpdateTarget records a newer sync target. It cancels the active session so
// the outer loop restarts with the new root.
// It is thread-safe, non-blocking, and monotonic (stale heights are ignored).
func (d *HashDBDynamicSyncer) UpdateTarget(newTarget message.Syncable) error {
	d.targetMu.Lock()
	defer d.targetMu.Unlock()

	newHeight := newTarget.Height()
	if newHeight <= d.desiredHeight {
		return nil
	}

	newRoot := newTarget.GetBlockRoot()
	if newRoot == d.desiredRoot {
		d.desiredHeight = newHeight
		return nil
	}

	d.desiredRoot = newRoot
	d.desiredHeight = newHeight

	if d.sessionCancel != nil {
		d.sessionCancel(errPivotRequested)
	}
	return nil
}

// setSessionCancel stores the cancel function used by UpdateTarget to trigger pivots.
func (d *HashDBDynamicSyncer) setSessionCancel(cancel context.CancelCauseFunc) {
	d.targetMu.Lock()
	defer d.targetMu.Unlock()
	d.sessionCancel = cancel
}

// getDesiredRoot returns the current desired root under the target mutex.
func (d *HashDBDynamicSyncer) getDesiredRoot() common.Hash {
	d.targetMu.Lock()
	defer d.targetMu.Unlock()
	return d.desiredRoot
}
