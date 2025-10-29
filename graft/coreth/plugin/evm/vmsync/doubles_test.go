// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmsync

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"

	syncpkg "github.com/ava-labs/avalanchego/graft/coreth/sync"
)

// FuncSyncer adapts a function to the simple Syncer shape used in tests. It is
// useful for defining small, behavior-driven syncers inline.
type FuncSyncer struct {
	fn func(ctx context.Context) error
}

// Sync calls the wrapped function and returns its result.
func (f FuncSyncer) Sync(ctx context.Context) error { return f.fn(ctx) }

// Name returns the provided name or a default if unspecified.
func (FuncSyncer) Name() string                          { return "Test Name" }
func (FuncSyncer) ID() string                            { return "test_id" }
func (FuncSyncer) UpdateTarget(_ message.Syncable) error { return nil }

var _ syncpkg.Syncer = FuncSyncer{}

// NewBarrierSyncer returns a syncer that signals startedWG.Done() when Sync begins,
// then blocks until releaseCh is closed (returns nil) or ctx is canceled (returns ctx.Err).
func NewBarrierSyncer(startedWG *sync.WaitGroup, releaseCh <-chan struct{}) FuncSyncer {
	return FuncSyncer{fn: func(ctx context.Context) error {
		if startedWG != nil {
			startedWG.Done()
		}
		select {
		case <-releaseCh:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
}

// NewErrorSyncer returns a syncer that signals startedWG.Done() when Sync begins,
// then blocks until trigger is closed (returns errToReturn) or ctx is canceled (returns ctx.Err).
func NewErrorSyncer(startedWG *sync.WaitGroup, trigger <-chan struct{}, errToReturn error) FuncSyncer {
	return FuncSyncer{fn: func(ctx context.Context) error {
		if startedWG != nil {
			startedWG.Done()
		}
		select {
		case <-trigger:
			return errToReturn
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
}

// NewCancelAwareSyncer returns a syncer that signals startedWG.Done() when Sync begins,
// then blocks until ctx is canceled (returns ctx.Err) or timeout elapses (returns timeout error).
func NewCancelAwareSyncer(startedWG *sync.WaitGroup, timeout time.Duration) FuncSyncer {
	return FuncSyncer{fn: func(ctx context.Context) error {
		if startedWG != nil {
			startedWG.Done()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(timeout):
			return errors.New("syncer timed out waiting for cancellation")
		}
	}}
}
