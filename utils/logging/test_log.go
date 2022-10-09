// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
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

func (NoLog) Fatal(string, ...zap.Field) {}

func (NoLog) Error(string, ...zap.Field) {}

func (NoLog) Warn(string, ...zap.Field) {}

func (NoLog) Info(string, ...zap.Field) {}

func (NoLog) Trace(string, ...zap.Field) {}

func (NoLog) Debug(string, ...zap.Field) {}

func (NoLog) Verbo(string, ...zap.Field) {}

func (NoLog) StopOnPanic() {}

func (NoLog) RecoverAndPanic(f func()) { f() }

func (NoLog) RecoverAndExit(f, exit func()) { defer exit(); f() }

func (NoLog) Stop() {}

type NoWarn struct{ NoLog }

func (NoWarn) Fatal(string, ...zap.Field) { panic("unexpected Fatal") }

func (NoWarn) Error(string, ...zap.Field) { panic("unexpected Error") }

func (NoWarn) Warn(string, ...zap.Field) { panic("unexpected Warn") }

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
func (discard) Close() error                { return nil }
