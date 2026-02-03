// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package loggingtest

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ io.WriteCloser = (*testWriter)(nil)

type testWriter struct {
	zaptest.TestingWriter
}

func (testWriter) Close() error {
	return nil
}

type testLogger struct {
	logging.Logger
	t testing.TB
}

// NewLogger returns a logger that writes to the provided testing.TB.
// Any logs will only be shown if the test fails.
// If Fatal is called, the test will be failed immediately.
// If Error or Warn are called, the test will be marked as failed, but will continue running.
func NewLogger(t testing.TB, logLevel logging.Level) logging.Logger {
	t.Helper()

	require.LessOrEqualf(t, logLevel, logging.Warn, "TestLogger will fail the test on Warn, but user asked to ignore %s logs", (logLevel - 1).String())

	w := testWriter{TestingWriter: zaptest.NewTestingWriter(t)}
	core := logging.NewWrappedCore(logLevel, w, logging.Plain.ConsoleEncoder())
	return &testLogger{
		Logger: logging.NewLogger("", core),
		t:      t,
	}
}

func (l *testLogger) Fatal(msg string, fields ...zap.Field) {
	l.Logger.Fatal(msg, fields...)
	l.t.Fatal("Fatal")
}

func (l *testLogger) Error(msg string, fields ...zap.Field) {
	l.Logger.Error(msg, fields...)
	l.t.Error("Error")
}

func (l *testLogger) Warn(msg string, fields ...zap.Field) {
	l.Logger.Warn(msg, fields...)
	l.t.Error("Warn")
}
