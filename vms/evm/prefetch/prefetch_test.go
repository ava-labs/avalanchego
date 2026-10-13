// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package prefetch

import (
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/state/snapshot"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/triedb"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// TestBoundedWorkersExecutesAllWork asserts that every function passed to
// [boundedWorkers.Execute] is run, and that [boundedWorkers.Done] doesn't
// return until they all have.
func TestBoundedWorkersExecutesAllWork(t *testing.T) {
	const (
		limit = 4
		work  = 100
	)
	b := newBoundedWorkers(limit)

	var completed atomic.Int64
	for range work {
		b.Execute(func() {
			completed.Add(1)
		})
	}
	b.Done()

	require.Equal(t, int64(work), completed.Load(), "work completed after %T.Done() returned", b)
}

// TestBoundedWorkersBoundsConcurrency asserts that [boundedWorkers] runs as
// many functions concurrently as it was configured to, but no more.
func TestBoundedWorkersBoundsConcurrency(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const limit = 4
		b := newBoundedWorkers(limit)

		var running atomic.Int64
		release := make(chan struct{})
		blockUntilReleased := func() {
			running.Add(1)
			<-release
		}

		for range limit {
			b.Execute(blockUntilReleased)
		}
		synctest.Wait()
		require.Equal(t, int64(limit), running.Load(), "functions running at the limit")

		// The next call must wait for a worker instead of starting one.
		submitted := make(chan struct{})
		go func() {
			defer close(submitted)
			b.Execute(blockUntilReleased)
		}()
		synctest.Wait()
		require.Equal(t, int64(limit), running.Load(), "functions running above the limit")

		// Releasing the workers lets the waiting call through.
		close(release)
		<-submitted
		b.Done()
	})
}

// TestBoundedWorkersDoneIsIdempotent asserts the documented guarantee that
// [boundedWorkers.Done] is safe to call more than once.
func TestBoundedWorkersDoneIsIdempotent(t *testing.T) {
	b := newBoundedWorkers(2)
	b.Execute(func() {})
	b.Done()

	require.NotPanics(t, b.Done, "second call to %T.Done()", b)
}

// TestWithConcurrentWorkersOptionIsReusable asserts that one
// [state.PrefetcherOption] can start any number of prefetchers. Each needs its
// own pool, because the first prefetcher to close shuts down a shared pool.
func TestWithConcurrentWorkersOptionIsReusable(t *testing.T) {
	// Run in a bubble so that a shared pool fails as an immediate deadlock
	// rather than hanging until the test binary times out.
	synctest.Test(t, func(t *testing.T) {
		opt := WithConcurrentWorkers(4)
		sdb := newStateDBWithSnapshot(t)

		for i := range 2 {
			sdb.StartPrefetcher("test", opt)
			// Dirtying an account makes Finalise() schedule prefetch work,
			// which is what exercises the pool.
			sdb.AddBalance(common.Address{byte(i + 1)}, uint256.NewInt(1))
			sdb.IntermediateRoot(false)
			sdb.StopPrefetcher()
		}
	})
}

// newStateDBWithSnapshot returns an empty [state.StateDB] backed by a snapshot,
// which [state.StateDB.StartPrefetcher] requires to start a prefetcher at all.
func newStateDBWithSnapshot(t *testing.T) *state.StateDB {
	t.Helper()

	disk := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(disk, nil)
	snaps, err := snapshot.New(snapshot.Config{CacheSize: 1}, disk, tdb, types.EmptyRootHash)
	require.NoError(t, err, "snapshot.New()")

	sdb, err := state.New(types.EmptyRootHash, state.NewDatabaseWithNodeDB(disk, tdb), snaps)
	require.NoError(t, err, "state.New()")
	return sdb
}
