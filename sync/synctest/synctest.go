// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package utilstest provides small testing helpers used across the codebase.
// It includes lightweight syncer adapters and utilities for coordinating
// goroutines under test.
package synctest

import (
	"context"
	"errors"
	"sync"
	"time"
)

// FuncSyncer adapts a function to the simple Syncer shape used in tests. It is
// useful for defining small, behavior-driven syncers inline.
type FuncSyncer struct {
	fn func(ctx context.Context) error
}

// Sync calls the wrapped function and returns its result.
func (f FuncSyncer) Sync(ctx context.Context) error { return f.fn(ctx) }

// NewBarrierSyncer returns a syncer that, upon entering Sync, calls wg.Done() to
// signal it has started, then blocks until either:
//   - `releaseCh` is closed, returning nil; or
//   - `ctx` is canceled, returning ctx.Err.
//
// This acts as a barrier to coordinate test goroutines.
func NewBarrierSyncer(wg *sync.WaitGroup, releaseCh <-chan struct{}) FuncSyncer {
	return FuncSyncer{fn: func(ctx context.Context) error {
		wg.Done()
		select {
		case <-releaseCh:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
}

// NewErrorSyncer returns a syncer that waits until either `trigger` is closed
// (then returns `errToReturn`) or `ctx` is canceled (then returns ctx.Err).
func NewErrorSyncer(trigger <-chan struct{}, errToReturn error) FuncSyncer {
	return FuncSyncer{fn: func(ctx context.Context) error {
		select {
		case <-trigger:
			return errToReturn
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
}

// NewCancelAwareSyncer closes `started` as soon as Sync begins, then waits for
// either:
//   - `ctx` cancellation: closes `canceled` and returns ctx.Err; or
//   - `timeout` elapsing: returns an error indicating a timeout.
//
// Useful for asserting that cancellation propagates to the syncer under test.
func NewCancelAwareSyncer(started, canceled chan struct{}, timeout time.Duration) FuncSyncer {
	return FuncSyncer{fn: func(ctx context.Context) error {
		close(started)
		select {
		case <-ctx.Done():
			close(canceled)
			return ctx.Err()
		case <-time.After(timeout):
			return errors.New("syncer timed out waiting for cancellation")
		}
	}}
}
