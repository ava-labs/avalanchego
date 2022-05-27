// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"errors"
	"io"
)

var (
	// Discard is a mock WriterCloser that drops all writes and close requests
	Discard io.WriteCloser = discard{}

	errNoLoggerWrite = errors.New("NoLogger can't write")

	_ Logger = NoLog{}
)

type NoLog struct{}

func (NoLog) Write([]byte) (int, error) { return 0, errNoLoggerWrite }

func (NoLog) Fatal(format string, args ...interface{}) {}

func (NoLog) Error(format string, args ...interface{}) {}

func (NoLog) Warn(format string, args ...interface{}) {}

func (NoLog) Info(format string, args ...interface{}) {}

func (NoLog) Trace(format string, args ...interface{}) {}

func (NoLog) Debug(format string, args ...interface{}) {}

func (NoLog) Verbo(format string, args ...interface{}) {}

func (NoLog) AssertNoError(error) {}

func (NoLog) AssertTrue(b bool, format string, args ...interface{}) {}

func (NoLog) AssertDeferredTrue(f func() bool, format string, args ...interface{}) {}

func (NoLog) AssertDeferredNoError(f func() error) {}

func (NoLog) StopOnPanic() {}

func (NoLog) RecoverAndPanic(f func()) { f() }

func (NoLog) RecoverAndExit(f, exit func()) { defer exit(); f() }

func (NoLog) Stop() {}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
func (discard) Close() error                { return nil }
