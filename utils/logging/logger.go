// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"io"

	"go.uber.org/zap"
)

// Func defines the method signature used for all logging methods on the Logger
// interface.
type Func func(msg string, fields ...zap.Field)

// Logger defines the interface that is used to keep a record of all events that
// happen to the program
type Logger interface {
	io.Writer // For logging pre-formatted messages

	// Log that a fatal error has occurred. The program should likely exit soon
	// after this is called
	Fatal(msg string, fields ...zap.Field)
	// Log that an error has occurred. The program should be able to recover
	// from this error
	Error(msg string, fields ...zap.Field)
	// Log that an event has occurred that may indicate a future error or
	// vulnerability
	Warn(msg string, fields ...zap.Field)
	// Log an event that may be useful for a user to see to measure the progress
	// of the protocol
	Info(msg string, fields ...zap.Field)
	// Log an event that may be useful for understanding the order of the
	// execution of the protocol
	Trace(msg string, fields ...zap.Field)
	// Log an event that may be useful for a programmer to see when debugging the
	// execution of the protocol
	Debug(msg string, fields ...zap.Field)
	// Log extremely detailed events that can be useful for inspecting every
	// aspect of the program
	Verbo(msg string, fields ...zap.Field)

	// With creates a new child logger with provided structured fields added as context
	// It doesn't modify the parent logger
	With(fields ...zap.Field) Logger

	// WithOptions clones the current Logger, applies the supplied Options, and returns the resulting Logger.
	WithOptions(opts ...zap.Option) Logger

	// SetLevel that this logger should log to
	SetLevel(level Level)
	// Enabled returns true if the given level is at or above this level.
	Enabled(lvl Level) bool

	// Recovers a panic, logs the error, and rethrows the panic.
	StopOnPanic()
	// If a function panics, this will log that panic and then re-panic ensuring
	// that the program logs the error before exiting.
	RecoverAndPanic(f func())

	// If a function panics, this will log that panic and then call the exit
	// function, ensuring that the program logs the error, recovers, and
	// executes the desired exit function
	RecoverAndExit(f, exit func())

	// Stop this logger and write back all meta-data.
	Stop()
}
