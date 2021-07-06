// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"errors"
	"sync"
)

var errNoLoggerWrite = errors.New("NoLogger can't write")

// NoLog ...
type NoLog struct{}

func (NoLog) Write([]byte) (int, error) { return 0, errNoLoggerWrite }

// Fatal ...
func (NoLog) Fatal(format string, args ...interface{}) {}

// Error ...
func (NoLog) Error(format string, args ...interface{}) {}

// Warn ...
func (NoLog) Warn(format string, args ...interface{}) {}

// Info ...
func (NoLog) Info(format string, args ...interface{}) {}

// Trace ...
func (NoLog) Trace(format string, args ...interface{}) {}

// Debug ...
func (NoLog) Debug(format string, args ...interface{}) {}

// Verbo ...
func (NoLog) Verbo(format string, args ...interface{}) {}

// AssertNoError ...
func (NoLog) AssertNoError(error) {}

// AssertTrue ...
func (NoLog) AssertTrue(b bool, format string, args ...interface{}) {}

// AssertDeferredTrue ...
func (NoLog) AssertDeferredTrue(f func() bool, format string, args ...interface{}) {}

// AssertDeferredNoError ...
func (NoLog) AssertDeferredNoError(f func() error) {}

// StopOnPanic ...
func (NoLog) StopOnPanic() {}

// RecoverAndPanic ...
func (NoLog) RecoverAndPanic(f func()) { f() }

// RecoverAndExit ...
func (NoLog) RecoverAndExit(f, exit func()) { defer exit(); f() }

// Stop ...
func (NoLog) Stop() {}

// SetLogLevel ...
func (NoLog) SetLogLevel(Level) {}

// SetDisplayLevel ...
func (NoLog) SetDisplayLevel(Level) {}

// SetPrefix ...
func (NoLog) SetPrefix(string) {}

// SetLoggingEnabled ...
func (NoLog) SetLoggingEnabled(bool) {}

// SetDisplayingEnabled ...
func (NoLog) SetDisplayingEnabled(bool) {}

// SetContextualDisplayingEnabled ...
func (NoLog) SetContextualDisplayingEnabled(bool) {}

// NoIOWriter is a mock Writer that does not write to any underlying source
type NoIOWriter struct{}

// Initialize ...
func (nw *NoIOWriter) Initialize(Config) (int, error) { return 0, nil }

// Flush ...
func (nw *NoIOWriter) Flush() error { return nil }

// Write ...
func (nw *NoIOWriter) Write(p []byte) (int, error) { return len(p), nil }

// WriteString ...
func (nw *NoIOWriter) WriteString(s string) (int, error) { return len(s), nil }

// Close ...
func (nw *NoIOWriter) Close() error { return nil }

// Rotate ...
func (nw *NoIOWriter) Rotate() error { return nil }

// NewTestLog ...
func NewTestLog(config Config) (*Log, error) {
	l := &Log{
		config: config,
		writer: &NoIOWriter{},
	}
	l.needsFlush = sync.NewCond(&l.flushLock)

	l.wg.Add(1)

	go l.RecoverAndPanic(l.run)

	return l, nil
}
