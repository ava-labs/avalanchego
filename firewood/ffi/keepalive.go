// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

package ffi

// #include <stdlib.h>
// #include "firewood.h"
import "C"

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Lock ordering inside the keep-alive infrastructure:
//
//	lease.mu (per-handle)  before  keepAliveRegistry.mu (per-database)
//
// Concretely: every [lease.release] / [lease.releaseLocked] call holds
// lease.mu while invoking [keepAliveRegistry.removeAndDecr], which
// briefly takes registry.mu. Going the other way (registry first, then
// lease.mu) is what [lease.attach] does on the closed-registry path —
// but it drops registry.mu before invoking dropFn, so there is no
// simultaneous nested acquisition. [closeAndForceDrop] takes registry.mu
// only for the snapshot, releases it, and then invokes dropFns, which
// re-enter registry.mu via removeAndDecr. No path holds both locks at
// once.

// ErrDropped is the shared sentinel wrapped by every dropped-handle error
// in this package: [ErrDroppedRevision], [ErrDroppedReconstructed], and the
// unexported proposal and iterator equivalents. Callers that don't care
// which handle type was invalidated — for example after
// [WithForceCloseHandles] drops every outstanding handle — can match all
// of them with errors.Is(err, ErrDropped).
var ErrDropped = errors.New("already dropped")

type handle[T any] struct {
	// handle is an opaque pointer to the underlying Rust object. It should be
	// passed to the C FFI functions that operate on this type.
	//
	// It is not safe to call these methods with a nil handle.
	//
	// Calls to `C.fwd_free_X` will invalidate this handle, so it should not be
	// used after those calls.
	ptr     T
	dropped bool

	// lease keeps the parent database alive while this handle is in use.
	// It is initialized by [lease.attach] after [newHandle] returns, and
	// released when [Drop] is called.
	lease lease

	free func(T) C.VoidResult
}

// newHandle constructs an inactive handle: the C pointer and free
// function are set, but the lease is not yet attached to any registry.
// Callers must invoke [lease.attach] before the handle becomes visible,
// or the registry's count/dropFn invariants will be broken.
func newHandle[T any](ptr T, free func(T) C.VoidResult) *handle[T] {
	return &handle[T]{
		ptr:     ptr,
		free:    free,
		dropped: false,
	}
}

func drop[T any](h *handle[T]) {
	_ = h.Drop()
}

// Drop releases the C-side resource and releases this handle's lease on
// the parent database.
//
// Release is unconditional: even if the underlying free call returns an
// error, the lease is still released. The C pointer is cleared before the
// free call, so a subsequent Drop is a no-op rather than a retry against
// a possibly-corrupt pointer. Any free error is surfaced to the caller,
// which is then responsible for deciding what to do — but the caller does
// not need to worry about the database hanging on shutdown because a free
// errored.
//
// Idempotent: subsequent calls after the first return nil.
func (h *handle[T]) Drop() error {
	return h.lease.release(func() error {
		if h.dropped {
			return nil
		}

		// Mark dropped and clear the pointer *before* calling free, so that
		// even if free errors we never call it twice on the same pointer.
		ptr := h.ptr
		var zero T
		h.ptr = zero
		h.dropped = true

		if err := getErrorFromVoidResult(h.free(ptr)); err != nil {
			return fmt.Errorf("%w: %w", errFreeingValue, err)
		}
		return nil
	})
}

// keepAliveRegistry tracks every outstanding handle that holds a lease on a
// database. It owns the outstanding-handle count that powers
// [Database.Close]'s graceful wait alongside a map of drop callbacks that
// [WithForceCloseHandles] uses to release leases forcibly.
//
// All bookkeeping (count + waiters + map) lives here; the lease type
// holds only a back-reference for release. Map values are outer-type
// Drop functions (e.g. [Iterator.Drop], whose implementation also frees
// any borrowed batch).
//
// The count+waiters mechanism replaces sync.WaitGroup. WaitGroup.Wait
// has no context, so the conventional ctx-aware wrapper spawns a
// goroutine per call; if ctx fires first that goroutine leaks until
// every outstanding handle eventually releases — and the failure mode
// [Close] is trying to surface is exactly "handles never release". The
// explicit count drains via per-call channels that are unregistered on
// ctx cancellation, so a failed-and-retried Close costs nothing.
type keepAliveRegistry struct {
	mu sync.Mutex
	// count is the number of outstanding leases. Bumped by [lease.attach]
	// and [lease.attachUnregistered]; decremented by [lease.releaseLocked].
	count int
	// waiters are channels closed when count drops to zero. Each call to
	// [waitDrained] appends one and removes it on ctx cancellation.
	waiters []chan struct{}
	// handles maps a registered lease to its outer Drop function. Entries
	// are added in [lease.attach] and removed in [removeAndDecr] (called
	// from [lease.releaseLocked]).
	handles map[*lease]func() error
	// closed is set by [closeAndForceDrop] and prevents any subsequent
	// registration. Once closed, [lease.attach] invokes the dropFn itself
	// and returns errDBClosed so the caller does not need to clean up.
	closed bool
}

func newKeepAliveRegistry() *keepAliveRegistry {
	return &keepAliveRegistry{
		handles: make(map[*lease]func() error),
	}
}

// removeAndDecr deletes a lease from the registry and decrements the
// outstanding-handle count. If the count reaches zero, any pending
// waiters are released. Called from [lease.releaseLocked] on disown.
func (r *keepAliveRegistry) removeAndDecr(l *lease) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.handles, l)
	r.decrLocked()
}

// decrLocked decrements the outstanding-handle count and releases any
// pending waiters when it reaches zero. Caller must hold r.mu.
func (r *keepAliveRegistry) decrLocked() {
	r.count--
	if r.count < 0 {
		// A negative count means a lease was released twice without an
		// intervening attach — the bookkeeping that keeps Close from
		// freeing the Db under live handles is broken, so fail loudly
		// here rather than let a later Close proceed with handles alive.
		panic("ffi: keep-alive handle count underflow (lease released twice)")
	}
	if r.count == 0 {
		for _, w := range r.waiters {
			close(w)
		}
		r.waiters = nil
	}
}

// waitDrained blocks until the outstanding-handle count reaches zero or
// ctx is cancelled. Returns nil on drain, ctx.Err() on cancellation.
//
// No goroutine is spawned per call: the waiter is a single channel
// closed by [keepAliveRegistry.decrLocked] when count hits zero. On ctx
// cancellation the channel is unregistered from the waiters slice so the
// registry does not retain a reference to it.
func (r *keepAliveRegistry) waitDrained(ctx context.Context) error {
	r.mu.Lock()
	if r.count == 0 {
		r.mu.Unlock()
		return nil
	}
	w := make(chan struct{})
	r.waiters = append(r.waiters, w)
	r.mu.Unlock()

	select {
	case <-w:
		return nil
	case <-ctx.Done():
		r.mu.Lock()
		// w may already have been closed concurrently with ctx firing,
		// in which case it's no longer in r.waiters. Both paths are
		// fine — find-and-remove handles either case.
		for i, x := range r.waiters {
			if x == w {
				r.waiters = append(r.waiters[:i], r.waiters[i+1:]...)
				break
			}
		}
		r.mu.Unlock()
		return ctx.Err()
	}
}

// closeAndForceDrop closes the registry to new registrations and invokes
// the Drop callback for every currently-registered lease. It is the
// implementation behind [WithForceCloseHandles].
//
// Atomicity: the closed flag is set and the snapshot is taken under the
// same critical section, so any [lease.attach] call that observes the
// registry as still open will be visible in the snapshot, and any call
// that observes it as closed will refuse and Drop itself. This removes
// the race a snapshot-and-retry loop would have with derived-handle
// constructors (e.g. [Proposal.Propose], [Revision.Iter]) that hold only
// the parent's lease.mu.RLock rather than [Database.handleLock].
//
// Drop callbacks are invoked without the registry lock held; they
// re-enter the lock via [removeAndDecr] inside releaseLocked, which is
// why the snapshot is taken eagerly.
//
// Drainable on retry: if ctx expires partway through, accumulated drop
// errors are returned and the registry stays closed, but handles that
// were not yet dropped remain in the map. A subsequent call re-snapshots
// the remainder and continues draining, so a caller that hits a context
// deadline can retry [Database.Close] with a fresh context to finish.
//
// Invariant: the `closed` flag gates [lease.attach] (new registrations),
// not re-entry to this function. Do NOT add an early `if r.closed
// { return }` here — that would break the retry-after-ctx-cancel path,
// since a partially-drained registry is exactly the state where a fresh
// context needs to resume draining the remaining handles.
func (r *keepAliveRegistry) closeAndForceDrop(ctx context.Context) error {
	r.mu.Lock()
	r.closed = true
	drops := make([]func() error, 0, len(r.handles))
	for _, fn := range r.handles {
		drops = append(drops, fn)
	}
	r.mu.Unlock()

	var dropErr error
	for _, fn := range drops {
		if ctx.Err() != nil {
			// Stop draining; the handles not yet dropped stay registered, so
			// the caller's subsequent waitForKeepAlives reports them as
			// still live. Returning ctx.Err() here would double-wrap when
			// Close joins the two errors.
			return dropErr
		}
		if err := fn(); err != nil {
			dropErr = errors.Join(dropErr, err)
		}
	}
	return dropErr
}

// lease represents a single outstanding handle's claim on the parent
// database. It exists so that operations on the wrapping handle can be
// serialized against [Database.Close] / [WithForceCloseHandles]: every
// public method on a Proposal, Revision, Reconstructed, or Iterator
// takes mu.RLock for the duration of the call, and force-drop takes
// mu.Lock via [release] before tearing down the C handle.
//
// All bookkeeping (the outstanding-handle count, the registry map)
// lives in [keepAliveRegistry]; the lease holds only a back-reference
// so that [releaseLocked] can find the registry it needs to decrement.
type lease struct {
	mu sync.RWMutex
	// registry is the parent database's keep-alive registry. Set by
	// [lease.attach] or [lease.attachUnregistered], cleared in
	// [releaseLocked]; nil indicates the lease has already been released.
	registry *keepAliveRegistry
}

// attach atomically registers this lease with both the registry's
// outstanding-handle count and its drop map under a single registry.mu
// critical section. Either both succeed, or — on a registry that has
// already been closed via [closeAndForceDrop] — dropFn is invoked, the
// count is NOT incremented, and errDBClosed is returned (joined with
// any error dropFn produced).
//
// This is the single entry point for activating a new lease. Holding
// the count without being in the drop map is the latent failure mode
// that [closeAndForceDrop] cannot recover from, so the increment and
// the map insert must happen under the same critical section.
//
// Caller does not need to hold lease.mu — a fresh lease isn't visible
// to anyone else until attach returns. Caller does not need to clean
// up on error: on the closed-registry path attach invokes dropFn itself.
func (l *lease) attach(registry *keepAliveRegistry, dropFn func() error) error {
	registry.mu.Lock()
	if registry.closed {
		registry.mu.Unlock()
		// Call dropFn outside registry.mu to preserve the lease.mu →
		// registry.mu lock order (dropFn → handle.Drop → lease.release
		// takes lease.mu, which would then take registry.mu via
		// removeAndDecr). No count++ ran, so no decrement is needed.
		// errors.Join discards nil, so a successful dropFn yields just
		// errDBClosed.
		return errors.Join(errDBClosed, dropFn())
	}
	if l.registry != nil {
		registry.mu.Unlock()
		// attach must be called exactly once per lease. Reaching this is
		// always a package bug — either a constructor re-used a lease or
		// two goroutines raced on a lease that should be single-owner.
		panic("lease already attached")
	}
	registry.count++
	registry.handles[l] = dropFn
	l.registry = registry
	registry.mu.Unlock()
	return nil
}

// attachUnregistered increments the registry's outstanding-handle count
// but does NOT add this lease to the drop map. It exists for
// [RangeProof], which intentionally stays outside the registry so its
// runtime.SetFinalizer can collect the proof — a bound Free in the
// registry map would keep the proof reachable forever. Consequently
// [WithForceCloseHandles] will not auto-drop a still-referenced
// RangeProof; graceful [Database.Close] still waits on the count.
//
// Re-attaching the same lease to the same registry is a no-op. Unlike a
// registered handle (see [lease.attach]), which gets a fresh lease per
// construction, a RangeProof can legitimately reach this path more than once:
// [Database.VerifyRangeProof] prepares a proposal and may be called repeatedly
// on the same proof (the Rust side and the finalizer are already idempotent).
// Making it idempotent keeps the count balanced — one attach, one release —
// rather than crashing on a redundant prepare. Re-attaching to a *different*
// registry is still a bug the count/registry bookkeeping cannot represent, so
// it panics.
//
// Callers must serialize against [Database.Close] (e.g. via
// db.handleLock.RLock) before invoking this — there is no closed-registry
// guard here. The change-proof family is being redesigned, so a
// handle[T] migration here is deferred.
func (l *lease) attachUnregistered(registry *keepAliveRegistry) {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	if l.registry == registry {
		// Already attached to this registry — idempotent; do not double-count.
		return
	}
	if l.registry != nil {
		panic("lease already attached to a different registry")
	}
	registry.count++
	l.registry = registry
}

// release runs attemptDisown and releases the lease via [releaseLocked].
//
// The release is unconditional — even when attemptDisown errors or
// panics. Keeping the lease alive on a free error would mean the parent
// database can never gracefully close, since [Database.Close] waits on a
// count that only decrements via release.
//
// Safe to call multiple times; subsequent calls after the first continue
// to invoke attemptDisown but do not double-decrement the count unless
// [attach] or [attachUnregistered] runs again in between.
func (l *lease) release(attemptDisown func() error) (err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Release on panic too, so a panicking callback cannot strand the
	// lease and block Close forever.
	defer func() {
		if p := recover(); p != nil {
			l.releaseLocked()
			panic(p)
		}
	}()
	err = attemptDisown()
	l.releaseLocked()
	return err
}

// releaseLocked is the single bookkeeping point: decrements the
// registry's outstanding-handle count, removes this lease from the
// registry, and clears the registry back-pointer. Caller must hold
// l.mu.
//
// Exists for callers like [Reconstructed.Reconstruct] that hold mu.Lock
// for a wider critical section and would otherwise deadlock through
// [release]. Idempotent: calls after the first are no-ops until
// [lease.attach] or [lease.attachUnregistered] runs again.
func (l *lease) releaseLocked() {
	if l.registry == nil {
		return
	}
	l.registry.removeAndDecr(l)
	l.registry = nil
}
