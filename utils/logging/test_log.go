// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"errors"
	"io"

	"go.uber.org/zap"
)

var (
	// Discard is a mock WriterCloser that drops all writes and close requests
	Discard io.WriteCloser = discard{}

	errNoLoggerWrite = errors.New("NoLogger can't write")

	_ Logger = NoLog{}
)

type NoLog struct{}

func (NoLog) Write([]byte) (int, error) { return 0, errNoLoggerWrite }

func (NoLog) Fatal(format string, args ...zap.Field) {}

func (NoLog) Error(format string, args ...zap.Field) {}

func (NoLog) Warn(format string, args ...zap.Field) {}

func (NoLog) Info(format string, args ...zap.Field) {}

func (NoLog) Trace(format string, args ...zap.Field) {}

func (NoLog) Debug(format string, args ...zap.Field) {}

func (NoLog) Verbo(format string, args ...zap.Field) {}

func (NoLog) AssertNoError(error) {}

func (NoLog) AssertTrue(b bool, format string, args ...zap.Field) {}

func (NoLog) AssertDeferredTrue(f func() bool, format string, args ...zap.Field) {}

func (NoLog) AssertDeferredNoError(f func() error) {}

func (NoLog) StopOnPanic() {}

func (NoLog) RecoverAndPanic(f func()) { f() }

func (NoLog) RecoverAndExit(f, exit func()) { defer exit(); f() }

func (NoLog) Stop() {}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
func (discard) Close() error                { return nil }
