// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"fmt"
	"io"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var _ Logger = &log{}

type log struct {
	assertionsEnabled bool
	wrappedCores      []WrappedCore
	internalLogger    *zap.Logger
}

type WrappedCore struct {
	Core           zapcore.Core
	Writer         io.WriteCloser
	WriterDisabled bool
	AtomicLevel    zap.AtomicLevel
}

func NewWrappedCore(level Level, rw io.WriteCloser, encoder zapcore.Encoder) WrappedCore {
	atomicLevel := zap.NewAtomicLevelAt(zapcore.Level(level))

	core := zapcore.NewCore(encoder, zapcore.AddSync(rw), atomicLevel)
	return WrappedCore{AtomicLevel: atomicLevel, Core: core, Writer: rw}
}

func newZapLogger(prefix string, wrappedCores ...WrappedCore) *zap.Logger {
	cores := make([]zapcore.Core, len(wrappedCores))
	for i, wc := range wrappedCores {
		cores[i] = wc.Core
	}
	core := zapcore.NewTee(cores...)
	logger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(2))
	if prefix != "" {
		logger = logger.Named(prefix)
	}

	return logger
}

// New returns a new logger set up according to [config]
func NewLogger(assertionsEnabled bool, prefix string, wrappedCores ...WrappedCore) Logger {
	return &log{
		assertionsEnabled: assertionsEnabled,
		internalLogger:    newZapLogger(prefix, wrappedCores...),
		wrappedCores:      wrappedCores,
	}
}

func (l *log) Write(p []byte) (int, error) {
	for _, wc := range l.wrappedCores {
		if wc.WriterDisabled {
			continue
		}
		_, _ = wc.Writer.Write(p)
	}
	return len(p), nil
}

func (l *log) Stop() {
	for _, wc := range l.wrappedCores {
		wc.Writer.Close()
	}
}

// Should only be called from [Level] functions.
func (l *log) log(level Level, format string, args ...interface{}) {
	// This if check is only needed to avoid calling the (potentially expensive)
	// Sprintf. Once the Sprintf is removed, this check can be removed as well.
	if !l.internalLogger.Core().Enabled(zapcore.Level(level)) {
		return
	}
	// TODO: remove this Sprintf and convert args to fields to use in ce.Write()
	args = SanitizeArgs(args)
	msg := fmt.Sprintf(format, args...)
	if ce := l.internalLogger.Check(zapcore.Level(level), msg); ce != nil {
		ce.Write()
	}
}

func (l *log) Fatal(format string, args ...interface{}) {
	l.log(Fatal, format, args...)
}

func (l *log) Error(format string, args ...interface{}) {
	l.log(Error, format, args...)
}

func (l *log) Warn(format string, args ...interface{}) {
	l.log(Warn, format, args...)
}

func (l *log) Info(format string, args ...interface{}) {
	l.log(Info, format, args...)
}

func (l *log) Trace(format string, args ...interface{}) {
	l.log(Trace, format, args...)
}

func (l *log) Debug(format string, args ...interface{}) {
	l.log(Debug, format, args...)
}

func (l *log) Verbo(format string, args ...interface{}) {
	l.log(Verbo, format, args...)
}

func (l *log) AssertNoError(err error) {
	if err != nil {
		l.Fatal("%s", err)
	}
	if l.assertionsEnabled && err != nil {
		l.Stop()
		panic(err)
	}
}

func (l *log) AssertTrue(b bool, format string, args ...interface{}) {
	if !b {
		l.Fatal(format, args...)
	}
	if l.assertionsEnabled && !b {
		l.Stop()
		panic(fmt.Sprintf(format, args...))
	}
}

func (l *log) AssertDeferredTrue(f func() bool, format string, args ...interface{}) {
	// Note, the logger will only be notified here if assertions are enabled
	if l.assertionsEnabled && !f() {
		err := fmt.Sprintf(format, args...)
		l.Fatal(err)
		l.Stop()
		panic(err)
	}
}

func (l *log) AssertDeferredNoError(f func() error) {
	if l.assertionsEnabled {
		if err := f(); err != nil {
			l.Fatal("%s", err)
			l.Stop()
			panic(err)
		}
	}
}

func (l *log) StopOnPanic() {
	if r := recover(); r != nil {
		l.Fatal("Panicking due to:\n%s\nFrom:\n%s", r, Stacktrace{})
		l.Stop()
		panic(r)
	}
}

func (l *log) RecoverAndPanic(f func()) { defer l.StopOnPanic(); f() }

func (l *log) stopAndExit(exit func()) {
	if r := recover(); r != nil {
		l.Fatal("Panicking due to:\n%s\nFrom:\n%s", r, Stacktrace{})
		l.Stop()
		exit()
	}
}

func (l *log) RecoverAndExit(f, exit func()) { defer l.stopAndExit(exit); f() }
