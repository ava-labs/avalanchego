// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package loggingtest provides [logging.Logger] implementations for use in
// tests. [New] forwards logs to a [testing.TB], treating warnings and errors as
// test failures. [NewRecorder] captures logs in memory so tests can assert on
// what was logged without any output.
package loggingtest

import (
	"context"
	"runtime"
	"slices"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/ava-labs/avalanchego/utils/logging"
)

// logger is the common wrapper around [Recorder] and [Logger] handlers,
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

// NewRecorder constructs a new [Recorder] at the specified level.
func NewRecorder(level logging.Level) *Recorder {
	r := new(Recorder)
	r.logger = &logger{
		handler: r, // yes, the recursion is gross, but that's composition for you ¯\_(ツ)_/¯
		level:   level,
	}
	return r
}

// A Recorder is a [logging.Logger] that stores all logs as [Record]
// entries for inspection.
type Recorder struct {
	*logger
	Records []*Record
}

// A Record is a single entry in a [Recorder].
type Record struct {
	Level  logging.Level
	Msg    string
	Fields []zap.Field
}

func (l *Recorder) log(lvl logging.Level, msg string, fields ...zap.Field) {
	l.Records = append(l.Records, &Record{
		Level:  lvl,
		Msg:    msg,
		Fields: fields,
	})
}

// Filter returns the recorded logs for which `fn` returns true.
func (l *Recorder) Filter(fn func(*Record) bool) []*Record {
	var out []*Record
	for _, r := range l.Records {
		if fn(r) {
			out = append(out, r)
		}
	}
	return out
}

// At returns all recorded logs at the specified [logging.Level].
func (l *Recorder) At(lvl logging.Level) []*Record {
	return l.Filter(func(r *Record) bool { return r.Level == lvl })
}

// AtLeast returns all recorded logs at or above the specified [logging.Level].
func (l *Recorder) AtLeast(lvl logging.Level) []*Record {
	return l.Filter(func(r *Record) bool { return r.Level >= lvl })
}

// New constructs a logger that propagates logs to [testing.TB]. WARNING
// and ERROR logs are sent to [testing.TB.Errorf] while FATAL is sent to
// [testing.TB.Fatalf]. All other logs are sent to [testing.TB.Logf]. Although
// the level can be configured, it is silently capped at [logging.Warn].
//
//nolint:thelper // The outputs include the logging site while the TB site is most useful if here
func New(tb testing.TB, level logging.Level) *Logger {
	l := &Logger{tb: tb}
	l.logger = &logger{
		handler: l, // TODO(arr4n) remove the recursion here and in [LogRecorder]
		level:   min(level, logging.Warn),
	}
	return l
}

// Logger is a [logging.Logger] that propagates logs to [testing.TB].
type Logger struct {
	*logger
	tb      testing.TB
	onError []context.CancelFunc
}

// CancelOnError pipes `ctx` to and from [context.WithCancel], calling the
// [context.CancelFunc] after logs >= [logging.Error], and during [testing.TB]
// cleanup.
func (l *Logger) CancelOnError(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	l.onError = append(l.onError, cancel)
	l.tb.Cleanup(cancel)
	return ctx
}

func (l *Logger) log(lvl logging.Level, msg string, fields ...zap.Field) {
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
