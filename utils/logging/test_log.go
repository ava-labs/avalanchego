// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"errors"
	"sync"
)

var errNoLoggerWrite = errors.New("NoLogger can't write")

type NoLog struct{}

func (NoLog) Write([]byte) (int, error) { return 0, errNoLoggerWrite }

func (NoLog) Fatal(format string, args ...interface{}) {}

func (NoLog) Error(format string, args ...interface{}) {}

func (NoLog) Warn(format string, args ...interface{}) {}

func (NoLog) Info(format string, args ...interface{}) {}

func (NoLog) Trace(format string, args ...interface{}) {}

func (NoLog) Debug(format string, args ...interface{}) {}

func (NoLog) Verbo(format string, args ...interface{}) {}

func (NoLog) AssertNoError(error) {}

func (NoLog) AssertTrue(b bool, format string, args ...interface{}) {}

func (NoLog) AssertDeferredTrue(f func() bool, format string, args ...interface{}) {}

func (NoLog) AssertDeferredNoError(f func() error) {}

func (NoLog) StopOnPanic() {}

func (NoLog) RecoverAndPanic(f func()) { f() }

func (NoLog) RecoverAndExit(f, exit func()) { defer exit(); f() }

func (NoLog) Stop() {}

func (NoLog) SetLogLevel(Level) {}

func (NoLog) SetDisplayLevel(Level) {}

// GetLogLevel ...
func (NoLog) GetLogLevel() Level { return Off }

// GetDisplayLevel ...
func (NoLog) GetDisplayLevel() Level { return Off }

// SetPrefix ...
func (NoLog) SetPrefix(string) {}

func (NoLog) SetLoggingEnabled(bool) {}

func (NoLog) SetDisplayingEnabled(bool) {}

func (NoLog) SetContextualDisplayingEnabled(bool) {}

// NoIOWriter is a mock Writer that does not write to any underlying source
type NoIOWriter struct{}

func (nw *NoIOWriter) Initialize(Config) (int, error) { return 0, nil }

func (nw *NoIOWriter) Flush() error { return nil }

func (nw *NoIOWriter) Write(p []byte) (int, error) { return len(p), nil }

func (nw *NoIOWriter) WriteString(s string) (int, error) { return len(s), nil }

func (nw *NoIOWriter) Close() error { return nil }

func (nw *NoIOWriter) Rotate() error { return nil }

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
