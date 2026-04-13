// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saetest

import (
	"context"
	"runtime"
	"slices"
	"testing"

	"github.com/ava-labs/avalanchego/utils/logging"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// logger is the common wrapper around [LogRecorder] and [tbLogger] handlers,
// plumbing all levels into the handler.
type logger struct {
	level   logging.Level
	handler interface {
		log(logging.Level, string, ...zap.Field)
	}
	with []zap.Field
	// Some methods will panic, in which case they need to be implemented. This
	// is better than embedding a [logging.NoLog], which could silently drop
	// important entries.
	logging.Logger
}

var _ logging.Logger = (*logger)(nil)

func (l *logger) With(fields ...zap.Field) logging.Logger {
	return &logger{
		level:   l.level,
		handler: l.handler,
		with:    slices.Concat(l.with, fields),
	}
}

func (l *logger) log(lvl logging.Level, msg string, fields ...zap.Field) {
	if lvl < l.level {
		return
	}
	l.handler.log(lvl, msg, slices.Concat(l.with, fields)...)
}

func (l *logger) Debug(msg string, fs ...zap.Field) { l.log(logging.Debug, msg, fs...) }
func (l *logger) Trace(msg string, fs ...zap.Field) { l.log(logging.Trace, msg, fs...) }
func (l *logger) Info(msg string, fs ...zap.Field)  { l.log(logging.Info, msg, fs...) }
func (l *logger) Warn(msg string, fs ...zap.Field)  { l.log(logging.Warn, msg, fs...) }
func (l *logger) Error(msg string, fs ...zap.Field) { l.log(logging.Error, msg, fs...) }
func (l *logger) Fatal(msg string, fs ...zap.Field) { l.log(logging.Fatal, msg, fs...) }

// NewLogRecorder constructs a new [LogRecorder] at the specified level.
func NewLogRecorder(level logging.Level) *LogRecorder {
	r := new(LogRecorder)
	r.logger = &logger{
		handler: r, // yes, the recursion is gross, but that's composition for you ¯\_(ツ)_/¯
		level:   level,
	}
	return r
}

// A LogRecorder is a [logging.Logger] that stores all logs as [LogRecord]
// entries for inspection.
type LogRecorder struct {
	*logger
	Records []*LogRecord
}

// A LogRecord is a single entry in a [LogRecorder].
type LogRecord struct {
	Level  logging.Level
	Msg    string
	Fields []zap.Field
}

func (l *LogRecorder) log(lvl logging.Level, msg string, fields ...zap.Field) {
	l.Records = append(l.Records, &LogRecord{
		Level:  lvl,
		Msg:    msg,
		Fields: fields,
	})
}

// Filter returns the recorded logs for which `fn` returns true.
func (l *LogRecorder) Filter(fn func(*LogRecord) bool) []*LogRecord {
	var out []*LogRecord
	for _, r := range l.Records {
		if fn(r) {
			out = append(out, r)
		}
	}
	return out
}

// At returns all recorded logs at the specified [logging.Level].
func (l *LogRecorder) At(lvl logging.Level) []*LogRecord {
	return l.Filter(func(r *LogRecord) bool { return r.Level == lvl })
}

// AtLeast returns all recorded logs at or above the specified [logging.Level].
func (l *LogRecorder) AtLeast(lvl logging.Level) []*LogRecord {
	return l.Filter(func(r *LogRecord) bool { return r.Level >= lvl })
}

// NewTBLogger constructs a logger that propagates logs to [testing.TB]. WARNING
// and ERROR logs are sent to [testing.TB.Errorf] while FATAL is sent to
// [testing.TB.Fatalf]. All other logs are sent to [testing.TB.Logf]. Although
// the level can be configured, it is silently capped at [logging.Warn].
//
//nolint:thelper // The outputs include the logging site while the TB site is most useful if here
func NewTBLogger(tb testing.TB, level logging.Level) *TBLogger {
	l := &TBLogger{tb: tb}
	l.logger = &logger{
		handler: l, // TODO(arr4n) remove the recursion here and in [LogRecorder]
		level:   min(level, logging.Warn),
	}
	return l
}

// TBLogger is a [logging.Logger] that propagates logs to [testing.TB].
type TBLogger struct {
	*logger
	tb      testing.TB
	onError []context.CancelFunc
}

// CancelOnError pipes `ctx` to and from [context.WithCancel], calling the
// [context.CancelFunc] after logs >= [logging.Error], and during [testing.TB]
// cleanup.
func (l *TBLogger) CancelOnError(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	l.onError = append(l.onError, cancel)
	l.tb.Cleanup(cancel)
	return ctx
}

func (l *TBLogger) log(lvl logging.Level, msg string, fields ...zap.Field) {
	var to func(string, ...any)
	switch {
	case lvl == logging.Warn || lvl == logging.Error: // because @ARR4N says warnings in tests are errors
		to = l.tb.Errorf
	case lvl >= logging.Fatal:
		to = l.tb.Fatalf
	default:
		to = l.tb.Logf
	}

	defer func() {
		if lvl < logging.Error {
			return
		}
		for _, fn := range l.onError {
			fn()
		}
	}()

	enc := zapcore.NewMapObjectEncoder()
	for _, f := range fields {
		f.AddTo(enc)
	}
	_, file, line, _ := runtime.Caller(3)
	to("[Log@%s] %s %v - %s:%d", lvl, msg, enc.Fields, file, line)
}
