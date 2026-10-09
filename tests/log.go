// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"os"

	"github.com/ava-labs/avalanchego/utils/logging"
)

func NewDefaultLogger(prefix string) logging.Logger {
	return NewLogger(prefix, logging.Debug)
}

// NewLogger returns a logger for the auto-detected format that drops entries
// below level.
func NewLogger(prefix string, level logging.Level) logging.Logger {
	log, err := newLogger(prefix, logging.AutoString, level)
	if err != nil {
		// This should never happen since auto is a valid log format
		panic(err)
	}
	return log
}

// TODO(marun) Does/should the logging package have a function like this?
func LoggerForFormat(prefix string, rawLogFormat string) (logging.Logger, error) {
	return newLogger(prefix, rawLogFormat, logging.Debug)
}

func newLogger(prefix string, rawLogFormat string, level logging.Level) (logging.Logger, error) {
	writeCloser := os.Stdout
	logFormat, err := logging.ToFormat(rawLogFormat, writeCloser.Fd())
	if err != nil {
		return nil, err
	}
	return logging.NewLogger(prefix, logging.NewWrappedCore(level, writeCloser, logFormat.ConsoleEncoder())), nil
}
