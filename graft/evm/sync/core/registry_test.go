// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/sync/types"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/utils/utilstest"
)

var _ types.Syncer = (*mockSyncer)(nil)

// mockSyncer implements [syncpkg.Syncer] for testing.
type mockSyncer struct {
	name      string
	syncError error
	started   bool // Track if already started
}

func newMockSyncer(name string, syncError error) *mockSyncer {
	return &mockSyncer{name: name, syncError: syncError}
}

func (m *mockSyncer) Sync(context.Context) error {
	m.started = true
	return m.syncError
}

func (m *mockSyncer) Name() string { return m.name }
func (m *mockSyncer) ID() string   { return m.name }

// namedSyncer adapts an existing syncer with a provided name to satisfy Syncer with Name().
type namedSyncer struct {
	name   string
	syncer types.Syncer
}

func (n *namedSyncer) Sync(ctx context.Context) error { return n.syncer.Sync(ctx) }
func (n *namedSyncer) Name() string                   { return n.name }
func (n *namedSyncer) ID() string                     { return n.name }

// syncerConfig describes a test syncer setup for RunSyncerTasks table tests.
type syncerConfig struct {
	name      string
	syncError error
}

func TestNewSyncerRegistry(t *testing.T) {
	registry := NewSyncerRegistry()
	require.NotNil(t, registry)
	require.Empty(t, registry.syncers)
}

func TestSyncerRegistry_Register(t *testing.T) {
	tests := []struct {
		name          string
		registrations []*mockSyncer
		expectedError error
		expectedCount int
	}{
		{
			name: "successful registrations",
			registrations: []*mockSyncer{
				newMockSyncer("Syncer1", nil),
				newMockSyncer("Syncer2", nil),
			},
			expectedCount: 2,
		},
		{
			name: "duplicate id registration",
			registrations: []*mockSyncer{
				newMockSyncer("Syncer1", nil),
				newMockSyncer("Syncer1", nil),
			},
			expectedError: errSyncerAlreadyRegistered,
			expectedCount: 1,
		},
		{
			name: "preserve registration order",
			registrations: []*mockSyncer{
				newMockSyncer("Syncer1", nil),
				newMockSyncer("Syncer2", nil),
				newMockSyncer("Syncer3", nil),
			},
			expectedCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewSyncerRegistry()
			var errLast error

			// Perform registrations.
			for _, reg := range tt.registrations {
				err := registry.Register(reg)
				if err != nil {
					errLast = err
					break
				}
			}

			// Check error expectations.
			require.ErrorIs(t, errLast, tt.expectedError)

			// Verify registration count.
			require.Len(t, registry.syncers, tt.expectedCount)

			// Verify registration order for successful cases.
			if tt.expectedError == nil {
				for i, reg := range tt.registrations {
					require.Equal(t, reg.name, registry.syncers[i].name)
					require.Equal(t, reg, registry.syncers[i].syncer)
				}
			}
		})
	}
}

func TestSyncerRegistry_RunSyncerTasks(t *testing.T) {
	errFoo := errors.New("foo")
	tests := []struct {
		name          string
		syncers       []syncerConfig
		expectedError error
		assertState   func(t *testing.T, mockSyncers []*mockSyncer)
	}{
		{
			name: "successful execution",
			syncers: []syncerConfig{
				{"Syncer1", nil},
				{"Syncer2", nil},
			},
			assertState: func(t *testing.T, mockSyncers []*mockSyncer) {
				for i, mockSyncer := range mockSyncers {
					require.True(t, mockSyncer.started, "Syncer %d should have been started", i)
				}
			},
		}, {
			name: "error returned",
			syncers: []syncerConfig{
				{"Syncer1", errFoo},
				{"Syncer2", nil},
			},
			expectedError: errFoo,
			assertState: func(t *testing.T, mockSyncers []*mockSyncer) {
				// First syncer should be started and waited on (but wait failed).
				require.True(t, mockSyncers[0].started, "First syncer should have been started")
				// With concurrency, the second may or may not have started -> don't assert it.
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewSyncerRegistry()
			mockSyncers := make([]*mockSyncer, len(tt.syncers))

			// Register syncers.
			for i, syncerConfig := range tt.syncers {
				mockSyncer := newMockSyncer(syncerConfig.name, syncerConfig.syncError)
				mockSyncers[i] = mockSyncer
				require.NoError(t, registry.Register(mockSyncer))
			}

			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)

			err := registry.RunSyncerTasks(ctx, newTestClientSummary(t))

			require.ErrorIs(t, err, tt.expectedError)

			// Use custom assertion function for each test case.
			tt.assertState(t, mockSyncers)
		})
	}
}

func TestSyncerRegistry_ConcurrentStart(t *testing.T) {
	t.Parallel()

	registry := NewSyncerRegistry()

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	const numBarrierSyncers = 5

	var allStartedWG sync.WaitGroup
	allStartedWG.Add(numBarrierSyncers)

	releaseCh := make(chan struct{})

	for i := 0; i < numBarrierSyncers; i++ {
		name := fmt.Sprintf("BarrierSyncer-%d", i)
		s := &namedSyncer{name: name, syncer: NewBarrierSyncer(&allStartedWG, releaseCh)}
		require.NoError(t, registry.Register(s))
	}

	doneCh := startSyncersAsync(registry, ctx, newTestClientSummary(t))

	utilstest.WaitGroupWithTimeout(t, &allStartedWG, 2*time.Second, "timed out waiting for barrier syncers to start")
	close(releaseCh)

	require.NoError(t, utilstest.WaitErrWithTimeout(t, doneCh, 4*time.Second))
}

func TestSyncerRegistry_ErrorPropagatesAndCancelsOthers(t *testing.T) {
	t.Parallel()

	registry := NewSyncerRegistry()

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	// Error syncer
	trigger := make(chan struct{})
	errFirst := errors.New("test error")
	var errorSyncerStartedWG sync.WaitGroup
	errorSyncerStartedWG.Add(1)
	errorSyncer := &namedSyncer{name: "ErrorSyncer-0", syncer: NewErrorSyncer(&errorSyncerStartedWG, trigger, errFirst)}
	require.NoError(t, registry.Register(errorSyncer))

	// Cancel-aware syncers to verify cancellation propagation.
	const numCancelSyncers = 2
	startedWG := registerCancelAwareSyncers(t, registry, numCancelSyncers, 4*time.Second)

	doneCh := startSyncersAsync(registry, ctx, newTestClientSummary(t))

	// Ensure all syncers (error syncer and cancel-aware syncers) are running before triggering the error.
	utilstest.WaitGroupWithTimeout(t, &errorSyncerStartedWG, 2*time.Second, "timed out waiting for error syncer to start")
	utilstest.WaitGroupWithTimeout(t, startedWG, 2*time.Second, "timed out waiting for cancel-aware syncers to start")

	close(trigger)

	err := utilstest.WaitErrWithTimeout(t, doneCh, 4*time.Second)
	require.ErrorIs(t, err, errFirst)
}

func TestSyncerRegistry_FirstErrorWinsAcrossMany(t *testing.T) {
	t.Parallel()

	registry := NewSyncerRegistry()

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	const numErrorSyncers = 3

	var (
		triggers                 []chan struct{}
		errFirst                 error
		allErrorSyncersStartedWG sync.WaitGroup
	)
	allErrorSyncersStartedWG.Add(numErrorSyncers)

	for i := 0; i < numErrorSyncers; i++ {
		trigger := make(chan struct{})
		triggers = append(triggers, trigger)
		errInstance := errors.New("boom")
		if i == 0 {
			errFirst = errInstance
		}
		name := fmt.Sprintf("ErrorSyncer-%d", i)
		require.NoError(t, registry.Register(&namedSyncer{name: name, syncer: NewErrorSyncer(&allErrorSyncersStartedWG, trigger, errInstance)}))
	}

	doneCh := startSyncersAsync(registry, ctx, newTestClientSummary(t))

	// Wait for all error syncers to start before triggering the error.
	utilstest.WaitGroupWithTimeout(t, &allErrorSyncersStartedWG, 2*time.Second, "timed out waiting for error syncers to start")

	// Trigger only the first error - others should return due to cancellation.
	close(triggers[0])

	err := utilstest.WaitErrWithTimeout(t, doneCh, 4*time.Second)
	require.ErrorIs(t, err, errFirst)
}

func TestSyncerRegistry_NoSyncersRegistered(t *testing.T) {
	t.Parallel()

	registry := NewSyncerRegistry()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	require.NoError(t, registry.RunSyncerTasks(ctx, newTestClientSummary(t)))
}

func TestSyncerRegistry_ContextCancellationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		wantErr       error
		numSyncers    int
		syncerTimeout time.Duration
		timeout       time.Duration // Timeout duration (only used when wantErr is DeadlineExceeded)
	}{
		{
			name:          "context canceled during active sync",
			wantErr:       context.Canceled,
			numSyncers:    3,
			syncerTimeout: 5 * time.Second,
		},
		{
			name:          "context deadline exceeded",
			wantErr:       context.DeadlineExceeded,
			numSyncers:    1,
			syncerTimeout: 500 * time.Millisecond,
			timeout:       200 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry := NewSyncerRegistry()

			startedWG := registerCancelAwareSyncers(t, registry, tt.numSyncers, tt.syncerTimeout)

			ctx, cancel := newTestContext(t, tt.wantErr, tt.timeout)
			t.Cleanup(cancel)

			doneCh := startSyncersAsync(registry, ctx, newTestClientSummary(t))

			// Wait for syncers to start.
			waitTimeout := 2 * time.Second
			if tt.wantErr == context.DeadlineExceeded {
				// For deadline exceeded, use shorter timeout since deadline is tight.
				waitTimeout = 100 * time.Millisecond
			}
			utilstest.WaitGroupWithTimeout(t, startedWG, waitTimeout, "timed out waiting for syncers to start")

			// Trigger cancellation for Canceled test (DeadlineExceeded will expire naturally).
			if tt.wantErr == context.Canceled {
				cancel()
			}

			err := utilstest.WaitErrWithTimeout(t, doneCh, 4*time.Second)

			// Verify error propagates correctly.
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestSyncerRegistry_EarlyReturnOnAlreadyCancelledContext(t *testing.T) {
	t.Parallel()

	registry := NewSyncerRegistry()

	// Create and immediately cancel context.
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // Cancel before starting syncers to test early return.

	// Register syncers (they should never start).
	const numSyncers = 2
	mockSyncers := make([]*mockSyncer, numSyncers)
	for i := 0; i < numSyncers; i++ {
		name := fmt.Sprintf("Syncer-%d", i)
		mockSyncer := newMockSyncer(name, nil)
		mockSyncers[i] = mockSyncer
		require.NoError(t, registry.Register(mockSyncer))
	}

	// RunSyncerTasks should return immediately with context.Canceled.
	err := registry.RunSyncerTasks(ctx, newTestClientSummary(t))

	// Verify that context.Canceled is returned immediately.
	require.ErrorIs(t, err, context.Canceled)

	// Verify that syncers were never started (early return optimization).
	for i, mockSyncer := range mockSyncers {
		require.False(t, mockSyncer.started, "Syncer %d should not have been started due to early return", i)
	}
}

func TestSyncerRegistry_MixedCancellationAndSuccess(t *testing.T) {
	t.Parallel()

	registry := NewSyncerRegistry()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// Create a syncer that will succeed quickly.
	releaseCh := make(chan struct{})
	var successWG sync.WaitGroup
	successWG.Add(1)
	require.NoError(t, registry.Register(&namedSyncer{
		name:   "SuccessSyncer",
		syncer: NewBarrierSyncer(&successWG, releaseCh),
	}))

	// Create syncers that will be cancelled.
	const numCancelSyncers = 2
	startedWG := registerCancelAwareSyncers(t, registry, numCancelSyncers, 5*time.Second)

	doneCh := startSyncersAsync(registry, ctx, newTestClientSummary(t))

	utilstest.WaitGroupWithTimeout(t, &successWG, 2*time.Second, "success syncer did not start")
	utilstest.WaitGroupWithTimeout(t, startedWG, 2*time.Second, "timed out waiting for syncers to start")

	// Cancel context, this should cancel all syncers, even the one that could succeed.
	cancel()

	err := utilstest.WaitErrWithTimeout(t, doneCh, 4*time.Second)

	// Verify that the cancellation error is returned.
	require.ErrorIs(t, err, context.Canceled)
}

// startSyncersAsync starts the syncers asynchronously using StartAsync and returns a channel to receive the error.
func startSyncersAsync(registry *SyncerRegistry, ctx context.Context, summary message.Syncable) <-chan error {
	doneCh := make(chan error, 1)
	g := registry.StartAsync(ctx, summary)
	go func() {
		doneCh <- g.Wait()
	}()
	return doneCh
}

// registerCancelAwareSyncers registers [numSyncers] cancel-aware syncers with the registry
// and returns a WaitGroup to coordinate when syncers have started.
func registerCancelAwareSyncers(t *testing.T, registry *SyncerRegistry, numSyncers int, timeout time.Duration) *sync.WaitGroup {
	t.Helper()
	var startedWG sync.WaitGroup
	startedWG.Add(numSyncers)
	for i := 0; i < numSyncers; i++ {
		require.NoError(t, registry.Register(&namedSyncer{
			name:   fmt.Sprintf("Syncer-%d", i),
			syncer: NewCancelAwareSyncer(&startedWG, timeout),
		}))
	}
	return &startedWG
}

func newTestContext(t *testing.T, wantErr error, timeout time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	if wantErr == context.DeadlineExceeded {
		return context.WithTimeout(t.Context(), timeout)
	}
	return context.WithCancel(t.Context())
}

func newTestClientSummary(t *testing.T) message.Syncable {
	t.Helper()
	summary, err := message.NewBlockSyncSummary(common.HexToHash("0xdeadbeef"), 1000, common.HexToHash("0xdeadbeef"))
	require.NoError(t, err)

	return summary
}
