// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// blockingTest blocks its first run until released and then records the state
// of its context parent. Any further run blocks forever.
type blockingTest struct {
	started chan struct{} // closed when the first run begins
	release chan struct{} // closed by the test to let the first run finish

	runs        int
	ctxErr      error     // error of the context parent observed by the first run
	ctxDeadline time.Time // deadline of the context parent observed by the first run
}

func newBlockingTest() *blockingTest {
	return &blockingTest{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (b *blockingTest) Run(tc tests.TestContext, _ *Wallet) {
	b.runs++
	if b.runs > 1 {
		// An unexpected extra iteration deadlocks the synctest bubble and fails
		// fast instead of spinning.
		select {}
	}

	close(b.started)
	<-b.release
	ctx := tc.GetDefaultContextParent()
	b.ctxErr = ctx.Err()
	b.ctxDeadline, _ = ctx.Deadline()
}

// newTestLoadGenerator returns a generator with a single worker. The tests in
// this file never touch the wallet, so it is left nil.
func newTestLoadGenerator(test Test) LoadGenerator {
	return LoadGenerator{
		wallets: make([]*Wallet, 1),
		test:    test,
	}
}

// TestLoadGeneratorRunLoadTimeoutDoesNotCancelInFlightTest verifies that a test
// in flight when the load timeout fires completes on an uncancelled context
// bounded by testTimeout, and that no further iteration starts.
func TestLoadGeneratorRunLoadTimeoutDoesNotCancelInFlightTest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const (
			loadTimeout = time.Second
			testTimeout = time.Minute
		)

		test := newBlockingTest()
		generator := newTestLoadGenerator(test)

		start := time.Now()
		done := make(chan struct{})
		go func() {
			defer close(done)
			generator.Run(t.Context(), logging.NoLog{}, loadTimeout, testTimeout)
		}()

		// Let the first iteration start and park in flight, then advance past
		// the load timeout while it is still in flight.
		<-test.started
		time.Sleep(loadTimeout + time.Second)

		close(test.release)
		<-done

		// The in-flight test must not have been cancelled by the load timeout,
		// and its deadline must derive from testTimeout rather than loadTimeout.
		require.NoError(t, test.ctxErr)
		require.WithinDuration(t, start.Add(testTimeout), test.ctxDeadline, 0)
		// No new iteration may start once the load timeout has fired.
		require.Equal(t, 1, test.runs)
	})
}

// TestLoadGeneratorRunStopsWhenContextCancelled verifies that, without a load
// timeout, cancelling the caller's context still propagates to the in-flight
// test and causes Run to return without starting further iterations.
func TestLoadGeneratorRunStopsWhenContextCancelled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		test := newBlockingTest()
		generator := newTestLoadGenerator(test)

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			generator.Run(ctx, logging.NoLog{}, 0, time.Minute)
		}()

		<-test.started
		cancel()
		close(test.release)
		<-done

		require.ErrorIs(t, test.ctxErr, context.Canceled)
		require.Equal(t, 1, test.runs)
	})
}
