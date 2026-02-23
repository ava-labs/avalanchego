// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package engine

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
)

// FuncSyncer adapts a function to the simple Syncer shape used in tests. It is
// useful for defining small, behavior-driven syncers inline.
type FuncSyncer struct {
	name string
	id   string
	fn   func(ctx context.Context) error
}

// Sync calls the wrapped function and returns its result.
func (f FuncSyncer) Sync(ctx context.Context) error { return f.fn(ctx) }
func (f FuncSyncer) Name() string                   { return f.name }
func (f FuncSyncer) ID() string                     { return f.id }

var _ types.Syncer = FuncSyncer{}

// NewBarrierSyncer returns a syncer that signals startedWG.Done() when Sync begins,
// then blocks until releaseCh is closed (returns nil) or ctx is canceled (returns ctx.Err).
func NewBarrierSyncer(name string, startedWG *sync.WaitGroup, releaseCh <-chan struct{}) FuncSyncer {
	return FuncSyncer{name: name, id: name, fn: func(ctx context.Context) error {
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
func NewErrorSyncer(name string, startedWG *sync.WaitGroup, trigger <-chan struct{}, errToReturn error) FuncSyncer {
	return FuncSyncer{name: name, id: name, fn: func(ctx context.Context) error {
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
func NewCancelAwareSyncer(name string, startedWG *sync.WaitGroup, timeout time.Duration) FuncSyncer {
	return FuncSyncer{name: name, id: name, fn: func(ctx context.Context) error {
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
