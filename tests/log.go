// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"os"

	"github.com/ava-labs/avalanchego/utils/logging"
)

func NewDefaultLogger(prefix string) logging.Logger {
	log, err := LoggerForFormat(prefix, logging.AutoString)
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
	// TODO(marun) Make the log level configurable
	return logging.NewLogger(prefix, logging.NewWrappedCore(logging.Debug, writeCloser, logFormat.ConsoleEncoder())), nil
}
