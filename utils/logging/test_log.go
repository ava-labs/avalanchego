// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"io"

	"go.uber.org/zap"
)

var (
	// Discard is a mock WriterCloser that drops all writes and close requests
	Discard io.WriteCloser = discard{}

	_ Logger = NoLog{}
)

type NoLog struct{}

func (NoLog) Write(b []byte) (int, error) {
	return len(b), nil
}

func (NoLog) Fatal(string, ...zap.Field) {}

func (NoLog) Error(string, ...zap.Field) {}

func (NoLog) Warn(string, ...zap.Field) {}

func (NoLog) Info(string, ...zap.Field) {}

func (NoLog) Trace(string, ...zap.Field) {}

func (NoLog) Debug(string, ...zap.Field) {}

func (NoLog) Verbo(string, ...zap.Field) {}

func (n NoLog) With(...zap.Field) Logger {
	return n
}

func (n NoLog) WithOptions(...zap.Option) Logger {
	return n
}

func (NoLog) SetLevel(Level) {}

func (NoLog) Enabled(Level) bool {
	return false
}

func (NoLog) StopOnPanic() {}

func (NoLog) RecoverAndPanic(f func()) {
	f()
}

func (NoLog) RecoverAndExit(f, exit func()) {
	defer exit()
	f()
}

func (NoLog) Stop() {}

type NoWarn struct{ NoLog }

func (NoWarn) Fatal(string, ...zap.Field) {
	panic("unexpected Fatal")
}

func (NoWarn) Error(string, ...zap.Field) {
	panic("unexpected Error")
}

func (NoWarn) Warn(string, ...zap.Field) {
	panic("unexpected Warn")
}

type discard struct{}

func (discard) Write(p []byte) (int, error) {
	return len(p), nil
}

func (discard) Close() error {
	return nil
}
