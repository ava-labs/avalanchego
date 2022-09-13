// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
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
func (l *log) log(level Level, msg string, fields ...zap.Field) {
	if ce := l.internalLogger.Check(zapcore.Level(level), msg); ce != nil {
		ce.Write(fields...)
	}
}

func (l *log) Fatal(msg string, fields ...zap.Field) {
	l.log(Fatal, msg, fields...)
}

func (l *log) Error(msg string, fields ...zap.Field) {
	l.log(Error, msg, fields...)
}

func (l *log) Warn(msg string, fields ...zap.Field) {
	l.log(Warn, msg, fields...)
}

func (l *log) Info(msg string, fields ...zap.Field) {
	l.log(Info, msg, fields...)
}

func (l *log) Trace(msg string, fields ...zap.Field) {
	l.log(Trace, msg, fields...)
}

func (l *log) Debug(msg string, fields ...zap.Field) {
	l.log(Debug, msg, fields...)
}

func (l *log) Verbo(msg string, fields ...zap.Field) {
	l.log(Verbo, msg, fields...)
}

func (l *log) AssertNoError(err error) {
	if err != nil {
		l.Fatal("assertion failed", zap.Error(err))
	}
	if l.assertionsEnabled && err != nil {
		l.Stop()
		panic(err)
	}
}

func (l *log) AssertTrue(b bool, msg string, fields ...zap.Field) {
	if !b {
		l.Fatal(msg, fields...)
	}
	if l.assertionsEnabled && !b {
		l.Stop()
		panic(msg)
	}
}

func (l *log) AssertDeferredTrue(f func() bool, msg string, fields ...zap.Field) {
	// Note, the logger will only be notified here if assertions are enabled
	if l.assertionsEnabled && !f() {
		l.Fatal(msg, fields...)
		l.Stop()
		panic(msg)
	}
}

func (l *log) AssertDeferredNoError(f func() error) {
	if l.assertionsEnabled {
		if err := f(); err != nil {
			l.AssertNoError(err)
		}
	}
}

func (l *log) StopOnPanic() {
	if r := recover(); r != nil {
		l.Fatal("panicking", zap.Any("reason", r), zap.Stack("from"))
		l.Stop()
		panic(r)
	}
}

func (l *log) RecoverAndPanic(f func()) { defer l.StopOnPanic(); f() }

func (l *log) stopAndExit(exit func()) {
	if r := recover(); r != nil {
		l.Fatal("panicking", zap.Any("reason", r), zap.Stack("from"))
		l.Stop()
		exit()
	}
}

func (l *log) RecoverAndExit(f, exit func()) { defer l.stopAndExit(exit); f() }
