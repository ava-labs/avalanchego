// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutil

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

const (
	LOG_LEVEL      = "log_level"
	INFO_LOG_LEVEL = "info"
)

type TestLogger struct {
	*zap.Logger
	t                  *testing.T
	traceVerboseLogger *zap.Logger
	panicOnError       bool
	panicOnWarn        bool
	atomicLevel        zap.AtomicLevel
}

// keywordFilterCore is a zapcore.Core wrapper that only logs entries whose
// message contains at least one of the configured keywords.
type keywordFilterCore struct {
	zapcore.Core
	keywords []string
}

func (k keywordFilterCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	for _, kw := range k.keywords {
		if strings.Contains(ent.Message, kw) {
			return k.Core.Check(ent, ce)
		}
	}
	// If no keyword matches, drop the entry by returning ce unchanged.
	return ce
}

func (tl *TestLogger) Intercept(hook func(entry zapcore.Entry) error) {
	logger := tl.WithOptions(zap.Hooks(hook))
	tl.Logger = logger
}

func (tl *TestLogger) Silence() {
	tl.atomicLevel.SetLevel(zapcore.FatalLevel)
}

// SilenceExceptKeywords silences all logs EXCEPT those whose message contains
// at least one of the provided keywords.
func (tl *TestLogger) SilenceExceptKeywords(keywords ...string) {
	core := tl.Core()
	filteredCore := keywordFilterCore{
		Core:     core,
		keywords: keywords,
	}
	tl.Logger = zap.New(filteredCore, zap.AddCaller())
	tl.traceVerboseLogger = zap.New(filteredCore, zap.AddCaller())
}

func (tl *TestLogger) SetPanicOnError(panicOnError bool) {
	tl.panicOnError = panicOnError
}

func (tl *TestLogger) SetPanicOnWarn(panicOnWarn bool) {
	tl.panicOnWarn = panicOnWarn
}

func (tl *TestLogger) Trace(msg string, fields ...zap.Field) {
	tl.traceVerboseLogger.Log(zapcore.DebugLevel, msg, fields...)
}

func (tl *TestLogger) Verbo(msg string, fields ...zap.Field) {
	tl.traceVerboseLogger.Log(zapcore.DebugLevel, msg, fields...)
}

func (tl *TestLogger) Warn(msg string, fields ...zap.Field) {
	tl.Logger.Warn(msg, fields...)
	if tl.panicOnWarn {
		panicMsg := fmt.Sprintf("WARN during test %s: %s", tl.t.Name(), msg)
		panic(panicMsg)
	}
}

func (tl *TestLogger) Error(msg string, fields ...zap.Field) {
	tl.Logger.Error(msg, fields...)
	if tl.panicOnError {
		panicMsg := fmt.Sprintf("ERROR during test %s: %s", tl.t.Name(), msg)
		panic(panicMsg)
	}
}

func MakeLogger(t *testing.T, node ...int) *TestLogger {
	// Preserve existing behavior: logs to stdout by default.
	return MakeLoggerWithFile(t, nil, true, node...)
}

// MakeLoggerWithFile creates a TestLogger that can write to a file and optionally to stdout.
// - If writeStdout is true, logs may be written to stdout.
// - If fileWriter is non-nil, logs may be written to that fileWriter.
// - If both are enabled, logs go to both.
// - If neither is enabled, logs are discarded.
func MakeLoggerWithFile(t *testing.T, fileWriter zapcore.WriteSyncer, writeStdout bool, node ...int) *TestLogger {
	defaultEncoderConfig := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
	config := defaultEncoderConfig
	config.EncodeLevel = func(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString(strings.ToUpper(l.String()))
	}
	config.EncodeTime = zapcore.TimeEncoderOfLayout("[01-02|15:04:05.000]")
	config.ConsoleSeparator = " "

	atomicLevel := zap.NewAtomicLevelAt(zapcore.DebugLevel)

	var cores []zapcore.Core

	// Stdout core only if explicitly enabled
	if writeStdout {
		stdoutEncoder := zapcore.NewConsoleEncoder(config)
		if strings.ToLower(os.Getenv("LOG_LEVEL")) == "info" {
			stdoutEncoder = &DebugSwallowingEncoder{
				consoleEncoder: stdoutEncoder,
				ObjectEncoder:  stdoutEncoder,
				pool:           buffer.NewPool(),
			}
		}
		stdoutCore := zapcore.NewCore(stdoutEncoder, zapcore.AddSync(os.Stdout), atomicLevel)
		cores = append(cores, stdoutCore)
	}

	// File core only if provided
	if fileWriter != nil {
		fileEncoder := zapcore.NewConsoleEncoder(config)
		if strings.ToLower(os.Getenv(LOG_LEVEL)) == INFO_LOG_LEVEL {
			fileEncoder = &DebugSwallowingEncoder{
				consoleEncoder: fileEncoder,
				ObjectEncoder:  fileEncoder,
				pool:           buffer.NewPool(),
			}
		}
		fileCore := zapcore.NewCore(fileEncoder, fileWriter, atomicLevel)
		cores = append(cores, fileCore)
	}

	// If neither stdout nor file enabled, discard logs.
	var core zapcore.Core
	switch len(cores) {
	case 0:
		discardEncoder := zapcore.NewConsoleEncoder(config)
		core = zapcore.NewCore(discardEncoder, zapcore.AddSync(io.Discard), atomicLevel)
	case 1:
		core = cores[0]
	default:
		core = zapcore.NewTee(cores...)
	}

	logger := zap.New(core, zap.AddCaller())
	logger = logger.With(zap.String("test", t.Name()))
	if len(node) > 0 {
		logger = logger.With(zap.Int("myNodeID", node[0]))
	}

	traceVerboseLogger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))
	traceVerboseLogger = traceVerboseLogger.With(zap.String("test", t.Name()))
	if len(node) > 0 {
		traceVerboseLogger = traceVerboseLogger.With(zap.Int("myNodeID", node[0]))
	}

	return &TestLogger{t: t, Logger: logger, traceVerboseLogger: traceVerboseLogger,
		atomicLevel: atomicLevel,
	}
}

type DebugSwallowingEncoder struct {
	zapcore.ObjectEncoder
	consoleEncoder zapcore.Encoder
	pool           buffer.Pool
}

func (dse *DebugSwallowingEncoder) Clone() zapcore.Encoder {
	return &DebugSwallowingEncoder{
		pool:           dse.pool,
		ObjectEncoder:  dse.ObjectEncoder,
		consoleEncoder: dse.consoleEncoder.Clone(),
	}
}

func (dse *DebugSwallowingEncoder) EncodeEntry(entry zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	if entry.Level == zapcore.DebugLevel {
		return dse.pool.Get(), nil
	}
	return dse.consoleEncoder.EncodeEntry(entry, fields)
}

func (tl *TestLogger) SetLevel(level zapcore.Level) {
	tl.atomicLevel.SetLevel(level)
}
