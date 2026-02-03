// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package loggingtest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
)

// mockT is a *testing.T that records whether certain methods were called, rather than actually
// marking the test as failed or logging anything.
type mockT struct {
	*testing.T
	failed  bool
	errored bool
	logged  bool
}

func (t *mockT) Clear() {
	t.failed = false
	t.errored = false
	t.logged = false
}

func (t *mockT) Fail() {
	t.errored = true
}

func (t *mockT) FailNow() {
	t.failed = true
}

func (t *mockT) Fatal(...interface{}) {
	t.failed = true
}

func (t *mockT) Fatalf(string, ...interface{}) {
	t.failed = true
}

func (t *mockT) Error(...interface{}) {
	t.errored = true
}

func (t *mockT) Errorf(string, ...interface{}) {
	t.errored = true
}

func (t *mockT) Log(...interface{}) {
	t.logged = true
}

func (t *mockT) Logf(string, ...interface{}) {
	t.logged = true
}

func TestTestLog(t *testing.T) {
	tests := []struct {
		name   string
		level  logging.Level
		call   func(logger logging.Logger)
		checkT func(t *mockT)
	}{
		{
			name: "Fatal",
			call: func(logger logging.Logger) {
				logger.Fatal("fatal message")
			},
			checkT: func(t *mockT) {
				require.True(t, t.failed)
			},
		},
		{
			name: "Error",
			call: func(logger logging.Logger) {
				logger.Error("error message")
			},
			checkT: func(t *mockT) {
				require.True(t, t.errored)
				require.False(t, t.failed)
			},
		},
		{
			name: "Warn",
			call: func(logger logging.Logger) {
				logger.Warn("warn message")
			},
			checkT: func(t *mockT) {
				require.True(t, t.errored)
				require.False(t, t.failed)
			},
		},
		{
			name: "Info",
			call: func(logger logging.Logger) {
				logger.Info("info message")
			},
			checkT: func(t *mockT) {
				require.True(t, t.logged)
				require.False(t, t.errored)
				require.False(t, t.failed)
			},
		},
		{
			name: "Debug",
			call: func(logger logging.Logger) {
				logger.Debug("debug message")
			},
			checkT: func(t *mockT) {
				require.False(t, t.logged)
				require.False(t, t.errored)
				require.False(t, t.failed)
			},
		},
		{
			name:  "Reduced level",
			level: logging.Debug,
			call: func(logger logging.Logger) {
				logger.Debug("debug message")
			},
			checkT: func(t *mockT) {
				require.True(t, t.logged)
				require.False(t, t.errored)
				require.False(t, t.failed)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockT{T: t}
			logger := NewLogger(mock, tt.level)
			tt.call(logger)
			tt.checkT(mock)
		})
	}
}
