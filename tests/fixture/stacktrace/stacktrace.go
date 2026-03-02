// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stacktrace

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
)

// If the environment variable STACK_TRACE_ERRORS=1 is set, errors
// passing through the functions defined in this package will have a
// stack trace added to them. The following equivalents to stdlib error
// functions are provided:
//
// - `fmt.Errorf` -> `stacktrace.Errorf`
// - `errors.New` -> `stacktrace.New`
//
// Additionally, a stack trace can be added to an existing error with
// `stacktrace.Wrap(err)`.

var stackTraceErrors bool

func init() {
	if os.Getenv("STACK_TRACE_ERRORS") == "1" {
		stackTraceErrors = true
	}
}

type StackTraceError struct {
	StackTrace []runtime.Frame
	Cause      error
}

func (e StackTraceError) Error() string {
	result := e.Cause.Error()
	if !stackTraceErrors {
		return result
	}

	var b strings.Builder
	b.WriteString(result)
	b.WriteString("\nStack trace:\n")
	for _, frame := range e.StackTrace {
		fmt.Fprintf(&b, "%s:%d: %s\n", frame.File, frame.Line, frame.Function)
	}
	return b.String()
}

func (e StackTraceError) Unwrap() error {
	return e.Cause
}

func New(msg string) error {
	if !stackTraceErrors {
		return errors.New(msg)
	}
	return wrap(errors.New(msg))
}

// Errorf adds a stack trace to the last argument provided if it is an
// error and stack traces are enabled.
func Errorf(format string, args ...any) error {
	if !stackTraceErrors {
		return fmt.Errorf(format, args...)
	}

	// Assume the last argument is an error requiring a stack trace if it is of type error
	err, ok := args[len(args)-1].(error)
	if !ok {
		return fmt.Errorf(format, args...)
	}

	newErr := fmt.Errorf(format, args...)

	// If there's already a StackTraceError, preserve its stack but update the cause
	var existingStackErr StackTraceError
	if errors.As(err, &existingStackErr) {
		existingStackErr.Cause = newErr
		return existingStackErr
	}

	// No stack trace exists, capture one now
	return wrap(newErr)
}

func Wrap(err error) error {
	if !stackTraceErrors {
		return err
	}
	return wrap(err)
}

// wrap adds a stack trace to err if stack traces are enabled and it
// doesn't already have one.
func wrap(err error) error {
	if err == nil {
		return nil
	}

	// If there's already a StackTraceError in the chain, just return it
	var existingStackErr StackTraceError
	if errors.As(err, &existingStackErr) {
		return err
	}

	// Need to capture a stack trace
	const depth = 32
	var pcs [depth]uintptr
	skip := 3 // skip wrap, New/Wrap/Errorf, and runtime.Callers
	n := runtime.Callers(skip, pcs[:])

	frames := runtime.CallersFrames(pcs[:n])
	var frameSlice []runtime.Frame

	for {
		frame, more := frames.Next()
		frameSlice = append(frameSlice, frame)
		if !more {
			break
		}
	}

	return StackTraceError{
		StackTrace: frameSlice,
		Cause:      err,
	}
}
