// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"slices"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/stretchr/testify/require"
)

type noopLogger struct{}

func (noopLogger) Fatal(string, ...zap.Field) {}
func (noopLogger) Error(string, ...zap.Field) {}
func (noopLogger) Warn(string, ...zap.Field)  {}
func (noopLogger) Info(string, ...zap.Field)  {}
func (noopLogger) Trace(string, ...zap.Field) {}
func (noopLogger) Debug(string, ...zap.Field) {}
func (noopLogger) Verbo(string, ...zap.Field) {}

const testName = "test"
const testRunInterval = 200 * time.Millisecond

func waitNoReceive[T any](t *testing.T, ch <-chan T) {
	select {
	case <-ch:
		t.Fatal("channel unexpectedly signaled")
	case <-time.After(defaultWaitDuration):
		// good
	}
}

func waitReceive[T any](t *testing.T, ch <-chan T) T {
	select {
	case v := <-ch:
		return v
	case <-time.After(defaultWaitDuration):
		t.Fatal("timed out waiting for signal")
		return *new(T)
	}
}

// Ensures tasks only run when at/after the runInterval boundary, and that
// "too-early" ticks are ignored by the runner loop.
func TestTimeoutHandlerRunsOnlyOnInterval(t *testing.T) {
	start := time.Now()

	ran := make(chan []uint64, 4)
	runner := func(ids []uint64) {
		// copy since ids is reused by caller
		cp := append([]uint64(nil), ids...)
		ran <- cp
	}

	h := NewTimeoutHandler(noopLogger{}, testName, start, testRunInterval, runner)
	defer h.Close()

	h.AddTask(1)

	// Too early: < runInterval since last tick -> should not run
	h.Tick(start.Add(testRunInterval / 2))
	waitNoReceive(t, ran)

	// Exactly at interval: should run once with id=1
	h.Tick(start.Add(testRunInterval))
	batch := waitReceive(t, ran)
	require.Equal(t, []uint64{1}, sorted(batch))

	h.AddTask(2)
	// Another tick but less than interval since the lastTickTime: should not run
	h.Tick(start.Add(testRunInterval + testRunInterval/2))
	waitNoReceive(t, ran)

	// At 2*interval: should run again
	h.Tick(start.Add(2 * testRunInterval))
	batch = waitReceive(t, ran)
	require.Equal(t, []uint64{1, 2}, sorted(batch))
}

// Add & Remove single task, verifying it stops running after removal.
func TestTimeoutHandler_AddThenRemoveTask(t *testing.T) {
	start := time.Now()

	ran := make(chan []uint64, 2)
	runner := func(ids []uint64) { ran <- append([]uint64(nil), ids...) }

	h := NewTimeoutHandler(noopLogger{}, testName, start, testRunInterval, runner)
	defer h.Close()

	h.AddTask(7)
	h.Tick(start.Add(testRunInterval))
	require.Equal(t, []uint64{7}, sorted(waitReceive(t, ran)))

	// Remove then tick again; nothing should run
	h.RemoveTask(7)
	h.Tick(start.Add(2 * testRunInterval))
	waitNoReceive(t, ran)
}

// Multiple tasks get batched and delivered together; removing one leaves the rest.
func TestTimeoutHandler_MultipleTasksBatchAndPersist(t *testing.T) {
	start := time.Now()

	ran := make(chan []uint64, 2)
	runner := func(ids []uint64) { ran <- append([]uint64(nil), ids...) }

	h := NewTimeoutHandler(noopLogger{}, testName, start, testRunInterval, runner)
	defer h.Close()

	h.AddTask(1)
	h.AddTask(2)
	h.AddTask(3)

	// First run: should see all three (order not guaranteed)
	h.Tick(start.Add(testRunInterval))
	got := sorted(waitReceive(t, ran))
	require.Equal(t, []uint64{1, 2, 3}, got)

	// Remove one; remaining should continue to run on next valid tick
	h.RemoveTask(2)
	h.Tick(start.Add(2 * testRunInterval))
	got = sorted(waitReceive(t, ran))
	require.Equal(t, []uint64{1, 3}, got)
}

// Adding the same task twice should not duplicate it in the batch (set semantics).
func TestTimeoutHandler_AddDuplicateTaskIsIdempotent(t *testing.T) {
	start := time.Now()

	ran := make(chan []uint64, 1)
	runner := func(ids []uint64) { ran <- append([]uint64(nil), ids...) }

	h := NewTimeoutHandler(noopLogger{}, testName, start, testRunInterval, runner)
	defer h.Close()

	h.AddTask(42)
	h.AddTask(42) // duplicate

	h.Tick(start.Add(testRunInterval))
	got := sorted(waitReceive(t, ran))
	require.Equal(t, []uint64{42}, got)
}

// RemoveOldTasks should drop tasks where shouldRemove(id, cutoff) == true.
// With lessUint(a,b), that means id < cutoff.
func TestTimeoutHandler_RemoveOldTasks(t *testing.T) {
	start := time.Now()

	ran := make(chan []uint64, 2)
	runner := func(ids []uint64) { ran <- append([]uint64(nil), ids...) }

	h := NewTimeoutHandler(noopLogger{}, testName, start, testRunInterval, runner)
	defer h.Close()

	for _, id := range []uint64{1, 2, 3, 4, 5} {
		h.AddTask(id)
	}

	// Remove everything with id < 3 -> removes 1,2 ; keeps 3,4,5
	h.RemoveOldTasks(func(id uint64, _ struct{}) bool {
		return id < 3
	})
	h.Tick(start.Add(testRunInterval))
	got := sorted(waitReceive(t, ran))
	require.Equal(t, []uint64{3, 4, 5}, got)

	// Now remove everything with id < 5 -> removes 3,4 ; keeps 5
	h.RemoveOldTasks(func(id uint64, _ struct{}) bool {
		return id < 5
	})
	h.Tick(start.Add(2 * testRunInterval))
	got = sorted(waitReceive(t, ran))
	require.Equal(t, []uint64{5}, got)
}

// After Close, the goroutine stops and further ticks should not cause runs.
func TestTimeoutHandler_CloseStopsRunner(t *testing.T) {
	start := time.Now()

	ran := make(chan []uint64, 1)
	runner := func(ids []uint64) { ran <- append([]uint64(nil), ids...) }

	h := NewTimeoutHandler(noopLogger{}, testName, start, testRunInterval, runner)

	h.Close()
	// Calls after Close should be safe and no-ops for scheduling.
	h.AddTask(9)
	h.Tick(start.Add(testRunInterval))
	waitNoReceive(t, ran)
}

// If ticks come in faster than runInterval, the second "too-soon" tick should
// not trigger execution (interval gating). This overlaps with RunsOnlyOnInterval,
// but emphasizes back-to-back ticks.
func TestTimeoutHandler_BackToBackTicksUnderIntervalDontRun(t *testing.T) {
	start := time.Now()

	ran := make(chan []uint64, 2)
	runner := func(ids []uint64) { ran <- append([]uint64(nil), ids...) }

	h := NewTimeoutHandler(noopLogger{}, testName, start, testRunInterval, runner)
	defer h.Close()

	h.AddTask(100)

	h.Tick(start.Add(testRunInterval)) // should run
	require.Equal(t, []uint64{100}, sorted(waitReceive(t, ran)))
	h.Tick(start.Add(testRunInterval + time.Millisecond)) // < interval since lastTickTime -> no run
	waitNoReceive(t, ran)

	// Next valid interval boundary -> runs again
	h.Tick(start.Add(2 * testRunInterval))
	require.Equal(t, []uint64{100}, sorted(waitReceive(t, ran)))
}

func sorted(v []uint64) []uint64 {
	sorted := append([]uint64(nil), v...)
	slices.Sort(sorted)
	return sorted
}
