// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"io"

	"go.uber.org/zap"
)

// Logger defines the interface for logging
type Logger interface {
	io.Writer

	Fatal(msg string, fields ...zap.Field)
	Error(msg string, fields ...zap.Field)
	Warn(msg string, fields ...zap.Field)
	Info(msg string, fields ...zap.Field)
	Trace(msg string, fields ...zap.Field)
	Debug(msg string, fields ...zap.Field)
	Verbo(msg string, fields ...zap.Field)

	With(fields ...zap.Field) Logger
	WithOptions(opts ...zap.Option) Logger

	SetLevel(level Level)
	Enabled(lvl Level) bool

	StopOnPanic()
	RecoverAndPanic(f func())
	RecoverAndExit(f, exit func())

	Stop()
}
