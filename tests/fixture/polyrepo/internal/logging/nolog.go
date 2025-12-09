// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import "go.uber.org/zap"

var _ Logger = NoLog{}

// NoLog is a no-op logger for testing
type NoLog struct{}

func (NoLog) Write(b []byte) (int, error) { return len(b), nil }
func (NoLog) Fatal(string, ...zap.Field)  {}
func (NoLog) Error(string, ...zap.Field)  {}
func (NoLog) Warn(string, ...zap.Field)   {}
func (NoLog) Info(string, ...zap.Field)   {}
func (NoLog) Trace(string, ...zap.Field)  {}
func (NoLog) Debug(string, ...zap.Field)  {}
func (NoLog) Verbo(string, ...zap.Field)  {}
func (n NoLog) With(...zap.Field) Logger  { return n }
func (n NoLog) WithOptions(...zap.Option) Logger { return n }
func (NoLog) SetLevel(Level)              {}
func (NoLog) Enabled(Level) bool          { return false }
func (NoLog) StopOnPanic()                {}
func (NoLog) RecoverAndPanic(f func())    { f() }
func (NoLog) RecoverAndExit(f, exit func()) {
	defer exit()
	f()
}
func (NoLog) Stop() {}
