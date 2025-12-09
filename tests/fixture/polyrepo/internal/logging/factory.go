// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Factory creates new instances of Logger
type Factory interface {
	Make(name string) (Logger, error)
}

type factory struct {
	config Config
}

// NewFactory returns a new instance of a Factory
func NewFactory(config Config) Factory {
	return &factory{config: config}
}

func (f *factory) Make(name string) (Logger, error) {
	zapLevel := levelToZap(f.config.DisplayLevel)

	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalColorLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		zapcore.AddSync(os.Stdout),
		zapLevel,
	)

	zapLogger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))
	return &zapLoggerWrapper{
		logger: zapLogger.Named(name),
		level:  f.config.DisplayLevel,
	}, nil
}

func levelToZap(level Level) zapcore.Level {
	switch level {
	case Verbo, Debug:
		return zapcore.DebugLevel
	case Trace, Info:
		return zapcore.InfoLevel
	case Warn:
		return zapcore.WarnLevel
	case Error:
		return zapcore.ErrorLevel
	case Fatal:
		return zapcore.FatalLevel
	default:
		return zapcore.InfoLevel
	}
}

type zapLoggerWrapper struct {
	logger *zap.Logger
	level  Level
}

func (z *zapLoggerWrapper) Write(b []byte) (int, error) {
	z.logger.Info(string(b))
	return len(b), nil
}

func (z *zapLoggerWrapper) Fatal(msg string, fields ...zap.Field) {
	z.logger.Fatal(msg, fields...)
}

func (z *zapLoggerWrapper) Error(msg string, fields ...zap.Field) {
	z.logger.Error(msg, fields...)
}

func (z *zapLoggerWrapper) Warn(msg string, fields ...zap.Field) {
	z.logger.Warn(msg, fields...)
}

func (z *zapLoggerWrapper) Info(msg string, fields ...zap.Field) {
	z.logger.Info(msg, fields...)
}

func (z *zapLoggerWrapper) Trace(msg string, fields ...zap.Field) {
	z.logger.Info(msg, fields...)
}

func (z *zapLoggerWrapper) Debug(msg string, fields ...zap.Field) {
	z.logger.Debug(msg, fields...)
}

func (z *zapLoggerWrapper) Verbo(msg string, fields ...zap.Field) {
	z.logger.Debug(msg, fields...)
}

func (z *zapLoggerWrapper) With(fields ...zap.Field) Logger {
	return &zapLoggerWrapper{
		logger: z.logger.With(fields...),
		level:  z.level,
	}
}

func (z *zapLoggerWrapper) WithOptions(opts ...zap.Option) Logger {
	return &zapLoggerWrapper{
		logger: z.logger.WithOptions(opts...),
		level:  z.level,
	}
}

func (z *zapLoggerWrapper) SetLevel(level Level) {
	z.level = level
}

func (z *zapLoggerWrapper) Enabled(lvl Level) bool {
	return lvl >= z.level
}

func (z *zapLoggerWrapper) StopOnPanic() {
	if r := recover(); r != nil {
		z.logger.Fatal("panic", zap.Any("error", r))
	}
}

func (z *zapLoggerWrapper) RecoverAndPanic(f func()) {
	defer func() {
		if r := recover(); r != nil {
			z.logger.Error("panic", zap.Any("error", r))
			panic(r)
		}
	}()
	f()
}

func (z *zapLoggerWrapper) RecoverAndExit(f, exit func()) {
	defer func() {
		if r := recover(); r != nil {
			z.logger.Error("panic", zap.Any("error", r))
		}
		exit()
	}()
	f()
}

func (z *zapLoggerWrapper) Stop() {
	_ = z.logger.Sync()
}
