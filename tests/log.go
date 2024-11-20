// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"io"
	"os"

	"go.uber.org/zap/zapcore"

	"github.com/ava-labs/avalanchego/utils/logging"
)

// Define a simple encoder config appropriate for testing
var simpleEncoderConfig = zapcore.EncoderConfig{
	TimeKey:       "",
	LevelKey:      "level",
	NameKey:       "",
	CallerKey:     "",
	MessageKey:    "msg",
	StacktraceKey: "stacktrace",
	// TODO(marun) Figure out why this is outputting e.g. `Level(-6)` instead of `INFO`
	EncodeLevel: zapcore.LowercaseLevelEncoder,
}

// NewSimpleLogger returns a logger with limited output for the specified WriteCloser
func NewSimpleLogger(writeCloser io.WriteCloser) logging.Logger {
	return logging.NewLogger(
		"",
		logging.NewWrappedCore(
			logging.Verbo,
			writeCloser,
			zapcore.NewConsoleEncoder(simpleEncoderConfig),
		),
	)
}

func NewDefaultLogger(prefix string) logging.Logger {
	log, err := LoggerForFormat(prefix, "auto")
	if err != nil {
		// This should never happen since auto is a valid log format
		panic(err)
	}
	return log
}

// TODO(marun) Does/should the logging package have a function like this?
func LoggerForFormat(prefix string, rawLogFormat string) (logging.Logger, error) {
	writeCloser := os.Stdout
	logFormat, err := logging.ToFormat(rawLogFormat, writeCloser.Fd())
	if err != nil {
		return nil, err
	}
	return logging.NewLogger(prefix, logging.NewWrappedCore(logging.Verbo, writeCloser, logFormat.ConsoleEncoder())), nil
}
