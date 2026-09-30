// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

var _ TestContext = (*TBTestContext)(nil)

// TBTestContext implements TestContext over a testing.TB, so code written
// against TestContext also runs as a plain Go test.
type TBTestContext struct {
	tb  testing.TB
	log logging.Logger
}

// NewTBTestContext logs through tb.Log so output is attributed to the test.
func NewTBTestContext(tb testing.TB) *TBTestContext {
	return &TBTestContext{
		tb: tb,
		log: logging.NewLogger("", logging.NewWrappedCore(
			logging.Debug,
			tbWriter{tb: tb},
			logging.Plain.ConsoleEncoder(),
		)),
	}
}

func (tc *TBTestContext) Errorf(format string, args ...any) {
	tc.tb.Helper()
	tc.tb.Errorf(format, args...)
}

func (tc *TBTestContext) FailNow() {
	tc.tb.Helper()
	tc.tb.FailNow()
}

func (tc *TBTestContext) By(msg string, callback ...func()) {
	tc.tb.Helper()
	tc.log.Info("Step: " + msg)
	require.LessOrEqual(tc.tb, len(callback), 1)
	for _, cb := range callback {
		cb()
	}
}

func (tc *TBTestContext) DeferCleanup(cleanup func()) {
	tc.tb.Cleanup(cleanup)
}

func (tc *TBTestContext) Log() logging.Logger {
	return tc.log
}

func (tc *TBTestContext) ContextWithTimeout(duration time.Duration) context.Context {
	return ContextWithTimeout(tc, duration)
}

func (tc *TBTestContext) DefaultContext() context.Context {
	return DefaultContext(tc)
}

func (tc *TBTestContext) WithDefaultContext() common.Option {
	return WithDefaultContext(tc)
}

func (tc *TBTestContext) GetDefaultContextParent() context.Context {
	return tc.tb.Context()
}

// Eventually runs condition on the calling goroutine. testify's Eventually
// runs it on another goroutine, where FailNow cannot control the test.
func (tc *TBTestContext) Eventually(condition func() bool, waitFor time.Duration, tick time.Duration, msg string) {
	tc.tb.Helper()
	timer := time.NewTimer(waitFor)
	defer timer.Stop()
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	for {
		if condition() {
			return
		}
		select {
		case <-timer.C:
			tc.tb.Fatal(msg)
		case <-ticker.C:
		}
	}
}

type tbWriter struct {
	tb testing.TB
}

func (w tbWriter) Write(p []byte) (int, error) {
	w.tb.Log(strings.TrimSuffix(string(p), "\n"))
	return len(p), nil
}

func (tbWriter) Close() error {
	return nil
}
