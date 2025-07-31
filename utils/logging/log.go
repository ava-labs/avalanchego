// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"io"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var _ Logger = (*log)(nil)

type log struct {
	wrappedCores   []WrappedCore
	internalLogger *zap.Logger
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
func NewLogger(prefix string, wrappedCores ...WrappedCore) Logger {
	return &log{
		internalLogger: newZapLogger(prefix, wrappedCores...),
		wrappedCores:   wrappedCores,
	}
}

// TODO: return errors here
func (l *log) Write(p []byte) (int, error) {
	for _, wc := range l.wrappedCores {
		if wc.WriterDisabled {
			continue
		}
		_, _ = wc.Writer.Write(p)
	}
	return len(p), nil
}

// TODO: return errors here
func (l *log) Stop() {
	for _, wc := range l.wrappedCores {
		if wc.Writer != os.Stdout && wc.Writer != os.Stderr {
			_ = wc.Writer.Close()
		}
	}
}

// Enabled returns true if the given level is at or above this level.
func (l *log) Enabled(lvl Level) bool {
	return l.internalLogger.Level().Enabled(zapcore.Level(lvl))
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

func (l *log) With(fields ...zap.Field) Logger {
	return &log{
		internalLogger: l.internalLogger.With(fields...),
		wrappedCores:   l.wrappedCores,
	}
}

func (l *log) WithOptions(opts ...zap.Option) Logger {
	return &log{
		internalLogger: l.internalLogger.WithOptions(opts...),
		wrappedCores:   l.wrappedCores,
	}
}

func (l *log) SetLevel(level Level) {
	for _, core := range l.wrappedCores {
		core.AtomicLevel.SetLevel(zapcore.Level(level))
	}
}

func (l *log) StopOnPanic() {
	if r := recover(); r != nil {
		l.Fatal("panicking", zap.Any("reason", r), zap.Stack("from"))
		l.Stop()
		panic(r)
	}
}

func (l *log) RecoverAndPanic(f func()) {
	defer l.StopOnPanic()
	f()
}

func (l *log) stopAndExit(exit func()) {
	if r := recover(); r != nil {
		l.Fatal("panicking", zap.Any("reason", r), zap.Stack("from"))
		l.Stop()
		exit()
	}
}

func (l *log) RecoverAndExit(f, exit func()) {
	defer l.stopAndExit(exit)
	f()
}
