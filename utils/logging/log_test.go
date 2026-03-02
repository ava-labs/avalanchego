// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type testLogWriter struct {
	logs [][]byte // Each element is a call to Write
}

func (w *testLogWriter) Write(p []byte) (int, error) {
	w.logs = append(w.logs, p)
	return len(p), nil
}

func (*testLogWriter) Close() error {
	return nil
}

func TestLog(t *testing.T) {
	log := NewLogger("", NewWrappedCore(Info, Discard, Plain.ConsoleEncoder()))

	recovered := new(bool)
	panicFunc := func() {
		panic("DON'T PANIC!")
	}
	exitFunc := func() {
		*recovered = true
	}
	log.RecoverAndExit(panicFunc, exitFunc)

	require.True(t, *recovered)
}

func TestWith(t *testing.T) {
	testWriter := &testLogWriter{}
	parentLogger := NewLogger("", NewWrappedCore(Info, testWriter, Plain.ConsoleEncoder()))

	childLogger := parentLogger.With(zap.Bool("bool", true))
	childLogger.Info("child message")
	require.Len(t, testWriter.logs, 1)
	require.Contains(t, string(testWriter.logs[0]), `"bool": true`)

	parentLogger.Info("parent message")
	require.Len(t, testWriter.logs, 2)
	require.NotContains(t, string(testWriter.logs[1]), `"bool": true`)
}

func TestWithOption(t *testing.T) {
	testWriter := &testLogWriter{}
	parentLogger := NewLogger("", NewWrappedCore(Info, testWriter, Plain.ConsoleEncoder()))

	hookTriggered := false

	hook := func(_ zapcore.Entry) error {
		hookTriggered = true
		return nil
	}

	childLogger := parentLogger.WithOptions(zap.Hooks(hook))

	parentLogger.Info("parent message")
	require.Len(t, testWriter.logs, 1)
	require.False(t, hookTriggered)

	childLogger.Info("child message")
	require.Len(t, testWriter.logs, 2)
	require.True(t, hookTriggered)
}
