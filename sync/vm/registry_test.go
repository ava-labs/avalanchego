// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"
	"errors"
	"testing"
	"time"

	synccommon "github.com/ava-labs/coreth/sync"
	"github.com/stretchr/testify/require"
)

// mockSyncer implements synccommon.Syncer for testing.
type mockSyncer struct {
	name           string
	startDelay     time.Duration
	waitDelay      time.Duration
	startError     error
	waitError      error
	startCalled    bool
	waitCalled     bool
	startCallCount int
	waitCallCount  int
	started        bool // Track if already started
}

func newMockSyncer(name string, startDelay, waitDelay time.Duration, startError, waitError error) *mockSyncer {
	return &mockSyncer{
		name:       name,
		startDelay: startDelay,
		waitDelay:  waitDelay,
		startError: startError,
		waitError:  waitError,
	}
}

func (m *mockSyncer) Start(ctx context.Context) error {
	if m.started {
		return synccommon.ErrSyncerAlreadyStarted
	}

	m.started = true
	m.startCalled = true
	m.startCallCount++
	if m.startDelay > 0 {
		time.Sleep(m.startDelay)
	}
	return m.startError
}

func (m *mockSyncer) Wait(ctx context.Context) error {
	m.waitCalled = true
	m.waitCallCount++
	if m.waitDelay > 0 {
		time.Sleep(m.waitDelay)
	}

	return m.waitError
}

func TestNewSyncerRegistry(t *testing.T) {
	registry := NewSyncerRegistry()
	require.NotNil(t, registry)
	require.Empty(t, registry.syncers)
}

func TestSyncerRegistry_Register(t *testing.T) {
	tests := []struct {
		name          string
		registrations []struct {
			name   string
			syncer *mockSyncer
		}
		expectedError string
		expectedCount int
	}{
		{
			name: "successful registrations",
			registrations: []struct {
				name   string
				syncer *mockSyncer
			}{
				{"Syncer1", newMockSyncer("TestSyncer1", 0, 0, nil, nil)},
				{"Syncer2", newMockSyncer("TestSyncer2", 0, 0, nil, nil)},
			},
			expectedError: "",
			expectedCount: 2,
		},
		{
			name: "duplicate name registration",
			registrations: []struct {
				name   string
				syncer *mockSyncer
			}{
				{"Syncer1", newMockSyncer("Syncer1", 0, 0, nil, nil)},
				{"Syncer1", newMockSyncer("Syncer1", 0, 0, nil, nil)},
			},
			expectedError: "syncer with name 'Syncer1' is already registered",
			expectedCount: 1,
		},
		{
			name: "preserve registration order",
			registrations: []struct {
				name   string
				syncer *mockSyncer
			}{
				{"Syncer1", newMockSyncer("Syncer1", 0, 0, nil, nil)},
				{"Syncer2", newMockSyncer("Syncer2", 0, 0, nil, nil)},
				{"Syncer3", newMockSyncer("Syncer3", 0, 0, nil, nil)},
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
				err := registry.Register(reg.name, reg.syncer)
				if err != nil {
					errLast = err
					break
				}
			}

			// Check error expectations.
			if tt.expectedError != "" {
				require.Error(t, errLast)
				require.Contains(t, errLast.Error(), tt.expectedError)
			} else {
				require.NoError(t, errLast)
			}

			// Verify registration count.
			require.Len(t, registry.syncers, tt.expectedCount)

			// Verify registration order for successful cases.
			if tt.expectedError == "" {
				for i, reg := range tt.registrations {
					require.Equal(t, reg.name, registry.syncers[i].name)
					require.Equal(t, reg.syncer, registry.syncers[i].syncer)
				}
			}
		})
	}
}

func TestSyncerRegistry_RunSyncerTasks(t *testing.T) {
	tests := []struct {
		name    string
		syncers []struct {
			name       string
			startDelay time.Duration
			waitDelay  time.Duration
			startError error
			waitError  error
		}
		expectedError string
		assertState   func(t *testing.T, mockSyncers []*mockSyncer, expectedError string)
	}{
		{
			name: "successful execution",
			syncers: []struct {
				name       string
				startDelay time.Duration
				waitDelay  time.Duration
				startError error
				waitError  error
			}{
				{"Syncer1", 0, 0, nil, nil},
				{"Syncer2", 0, 0, nil, nil},
			},
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, expectedError string) {
				for i, mockSyncer := range mockSyncers {
					require.True(t, mockSyncer.startCalled, "Syncer %d should have been started", i)
					require.True(t, mockSyncer.waitCalled, "Syncer %d should have been waited on", i)
				}
			},
		},
		{
			name: "start error stops execution",
			syncers: []struct {
				name       string
				startDelay time.Duration
				waitDelay  time.Duration
				startError error
				waitError  error
			}{
				{"Syncer1", 0, 0, errors.New("start failed"), nil},
				{"Syncer2", 0, 0, nil, nil},
			},
			expectedError: "failed to start Syncer1",
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, expectedError string) {
				// First syncer should be started but not waited on.
				require.True(t, mockSyncers[0].startCalled, "First syncer should have been started")
				require.False(t, mockSyncers[0].waitCalled, "First syncer should not have been waited on due to start error")
				// Second syncer should not be started.
				require.False(t, mockSyncers[1].startCalled, "Second syncer should not have been started")
				require.False(t, mockSyncers[1].waitCalled, "Second syncer should not have been waited on")
			},
		},
		{
			name: "wait error stops execution",
			syncers: []struct {
				name       string
				startDelay time.Duration
				waitDelay  time.Duration
				startError error
				waitError  error
			}{
				{"Syncer1", 0, 0, nil, errors.New("wait failed")},
				{"Syncer2", 0, 0, nil, nil},
			},
			expectedError: "Syncer1 failed",
			assertState: func(t *testing.T, mockSyncers []*mockSyncer, expectedError string) {
				// First syncer should be started and waited on (but wait failed).
				require.True(t, mockSyncers[0].startCalled, "First syncer should have been started")
				require.True(t, mockSyncers[0].waitCalled, "First syncer should have been waited on")
				// Second syncer should not be started.
				require.False(t, mockSyncers[1].startCalled, "Second syncer should not have been started")
				require.False(t, mockSyncers[1].waitCalled, "Second syncer should not have been waited on")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewSyncerRegistry()
			mockSyncers := make([]*mockSyncer, len(tt.syncers))

			// Register syncers.
			for i, syncerConfig := range tt.syncers {
				mockSyncer := newMockSyncer(
					syncerConfig.name,
					syncerConfig.startDelay,
					syncerConfig.waitDelay,
					syncerConfig.startError,
					syncerConfig.waitError,
				)
				mockSyncers[i] = mockSyncer
				require.NoError(t, registry.Register(syncerConfig.name, mockSyncer))
			}

			ctx := context.Background()
			mockClient := &client{}

			err := registry.RunSyncerTasks(ctx, mockClient)

			if tt.expectedError != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectedError)
			} else {
				require.NoError(t, err)
			}

			// Use custom assertion function for each test case.
			tt.assertState(t, mockSyncers, tt.expectedError)
		})
	}
}
