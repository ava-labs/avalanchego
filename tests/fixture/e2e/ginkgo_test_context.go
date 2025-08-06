// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	"context"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

var _ tests.TestContext = (*GinkgoTestContext)(nil)

type ginkgoWriteCloser struct{}

func (*ginkgoWriteCloser) Write(p []byte) (n int, err error) {
	// Add a leading space to better differentiate from other ginkgo output
	_, _ = ginkgo.GinkgoWriter.Write([]byte(" "))
	return ginkgo.GinkgoWriter.Write(p)
}

func (*ginkgoWriteCloser) Close() error {
	return nil
}

// Define a simple encoder config appropriate for logging with ginkgo
var ginkgoEncoderConfig = zapcore.EncoderConfig{
	// Time, name and caller are omitted for consistency with previous output.
	TimeKey:        "",
	LevelKey:       "level",
	NameKey:        "",
	CallerKey:      "",
	MessageKey:     "msg",
	StacktraceKey:  "stacktrace",
	EncodeLevel:    logging.ConsoleColorLevelEncoder,
	EncodeDuration: zapcore.StringDurationEncoder,
}

// NewGinkgoLogger returns a logger with limited output
func newGinkgoLogger(cfg zapcore.Encoder) logging.Logger {
	return logging.NewLogger(
		"",
		logging.NewWrappedCore(
			logging.Info,
			&ginkgoWriteCloser{},
			cfg,
		),
	)
}

type GinkgoTestContext struct {
	logger logging.Logger
}

// NewEventHandlerTestContext provides a logger with full output to
// account for the limited context otherwise provided in an event
// handler e.g. SynchronizedBeforeSuite.
func NewEventHandlerTestContext() *GinkgoTestContext {
	return &GinkgoTestContext{
		logger: newGinkgoLogger(logging.Auto.ConsoleEncoder()),
	}
}

// NewTestContext provides a logger with limited output to account for
// the context already provided by ginkgo for test logging.
func NewTestContext() *GinkgoTestContext {
	return &GinkgoTestContext{
		logger: newGinkgoLogger(
			zapcore.NewConsoleEncoder(ginkgoEncoderConfig),
		),
	}
}

func (*GinkgoTestContext) Errorf(format string, args ...interface{}) {
	ginkgo.GinkgoT().Errorf(format, args...)
}

func (*GinkgoTestContext) FailNow() {
	ginkgo.GinkgoT().FailNow()
}

func (tc *GinkgoTestContext) Log() logging.Logger {
	return tc.logger
}

func (*GinkgoTestContext) Cleanup() {
	// No-op - ginkgo does this automatically
}

func (*GinkgoTestContext) DeferCleanup(cleanup func()) {
	ginkgo.DeferCleanup(cleanup)
}

func (*GinkgoTestContext) By(text string, callback ...func()) {
	ginkgo.By(text, callback...)
}

// Helper simplifying use of a timed context by canceling the context on ginkgo teardown.
func (tc *GinkgoTestContext) ContextWithTimeout(duration time.Duration) context.Context {
	return tests.ContextWithTimeout(tc, duration)
}

// Helper simplifying use of a timed context configured with the default timeout.
func (tc *GinkgoTestContext) DefaultContext() context.Context {
	return tests.DefaultContext(tc)
}

// Helper simplifying use via an option of a timed context configured with the default timeout.
func (tc *GinkgoTestContext) WithDefaultContext() common.Option {
	return tests.WithDefaultContext(tc)
}

func (*GinkgoTestContext) GetDefaultContextParent() context.Context {
	return context.Background()
}

// Re-implementation of testify/require.Eventually that is compatible with ginkgo. testify's
// version calls the condition function with a goroutine and ginkgo assertions don't work
// properly in goroutines.
func (*GinkgoTestContext) Eventually(condition func() bool, waitFor time.Duration, tick time.Duration, msg string) {
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), waitFor)
	defer cancel()
	for !condition() {
		select {
		case <-ctx.Done():
			require.Fail(ginkgo.GinkgoT(), msg)
		case <-ticker.C:
		}
	}
}
