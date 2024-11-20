// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"io"
	"os"

	"go.uber.org/zap/zapcore"

	"github.com/ava-labs/avalanchego/utils/logging"
)

// Define a default encoder appropriate for testing
var defaultEncoderConfig = zapcore.EncoderConfig{
	TimeKey:       "",
	LevelKey:      "level",
	NameKey:       "",
	CallerKey:     "",
	MessageKey:    "msg",
	StacktraceKey: "stacktrace",
	// TODO(marun) Figure out why this is outputting e.g. `Level(-6)` instead of `INFO`
	EncodeLevel: zapcore.LowercaseLevelEncoder,
}

// NewDefaultTestLogger returns a logger that writes to stdout
func NewDefaultTestLogger() logging.Logger {
	return NewTestLogger(os.Stdout)
}

// NewDefaultTestLogger returns a logger for the specified WriteCloser
func NewTestLogger(writeCloser io.WriteCloser) logging.Logger {
	return logging.NewLogger(
		"",
		logging.NewWrappedCore(
			logging.Verbo,
			writeCloser,
			zapcore.NewConsoleEncoder(defaultEncoderConfig),
		),
	)
}
