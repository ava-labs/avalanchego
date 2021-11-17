// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"io"
)

// Logger defines the interface that is used to keep a record of all events that
// happen to the program
type Logger interface {
	io.Writer // For logging pre-formated messages

	// Log that a fatal error has occurred. The program should likely exit soon
	// after this is called
	Fatal(format string, args ...interface{})
	// Log that an error has occurred. The program should be able to recover
	// from this error
	Error(format string, args ...interface{})
	// Log that an event has occurred that may indicate a future error or
	// vulnerability
	Warn(format string, args ...interface{})
	// Log an event that may be useful for a user to see to measure the progress
	// of the protocol
	Info(format string, args ...interface{})
	// Log an event that may be useful for understanding the order of the
	// execution of the protocol
	Trace(format string, args ...interface{})
	// Log an event that may be useful for a programmer to see when debuging the
	// execution of the protocol
	Debug(format string, args ...interface{})
	// Log extremely detailed events that can be useful for inspecting every
	// aspect of the program
	Verbo(format string, args ...interface{})

	// If assertions are enabled, will result in a panic if err is non-nil
	AssertNoError(err error)
	// If assertions are enabled, will result in a panic if b is false
	AssertTrue(b bool, format string, args ...interface{})
	// If assertions are enabled, the function will be called and will result in
	// a panic the returned value is non-nil
	AssertDeferredNoError(f func() error)
	// If assertions are enabled, the function will be called and will result in
	//  a panic the returned value is false
	AssertDeferredTrue(f func() bool, format string, args ...interface{})

	// Recovers a panic, logs the error, and rethrows the panic.
	StopOnPanic()
	// If a function panics, this will log that panic and then re-panic ensuring
	// that the program logs the error before exiting.
	RecoverAndPanic(f func())

	// If a function panics, this will log that panic and then call the exit
	// function, ensuring that the program logs the error, recovers, and
	// executes the desired exit function
	RecoverAndExit(f, exit func())

	// Only events above or equal to the level set will be logged
	SetLogLevel(Level)
	// Only logged events above or equal to the level set will be logged
	SetDisplayLevel(Level)
	// Gets current LogLevel
	GetLogLevel() Level
	// Gets current DisplayLevel
	GetDisplayLevel() Level
	// Add a prefix to all logged messages
	SetPrefix(string)
	// Enable or disable logging
	SetLoggingEnabled(bool)
	// Enable or disable the display of logged events
	SetDisplayingEnabled(bool)
	// Enable or disable the display of contextual information for logged events
	SetContextualDisplayingEnabled(bool)

	// Stop this logger and write back all meta-data.
	Stop()
}

// RotatingWriter allows for rotating a stream writer
type RotatingWriter interface {
	// Creates the log file if it doesn't exist or resume writing to it if it does
	Initialize(Config) (int, error)
	// Flushes the writer
	Flush() error
	// Writes [b] to the log file
	Write(b []byte) (int, error)
	// Writes [s] to the log file
	WriteString(s string) (int, error)
	// Closes the log file
	Close() error
	// Rotates the log files. Always keeps the current log in the same file.
	// Rotated log files are stored as by appending an integer to the log file name,
	// from 1 to the RotationSize defined in the configuration. 1 being the most
	// recently rotated log file.
	Rotate() error
}
