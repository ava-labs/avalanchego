// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

var _ TestContext = (*SimpleTestContext)(nil)

const failNowMessage = "SimpleTestContext.FailNow called"

type ErrorfHandler func(format string, args ...any)

type PanicHandler func(any)

type SimpleTestContext struct {
	defaultContextParent context.Context
	log                  logging.Logger

	cleanupFuncs  []func()
	cleanupCalled bool

	errorfHandler ErrorfHandler
	panicHandler  PanicHandler
}

func NewTestContext(log logging.Logger) *SimpleTestContext {
	return NewTestContextWithArgs(context.Background(), log, nil, nil)
}

func NewTestContextWithArgs(
	ctx context.Context,
	log logging.Logger,
	errorfHandler ErrorfHandler,
	panicHandler PanicHandler,
) *SimpleTestContext {
	return &SimpleTestContext{
		defaultContextParent: ctx,
		log:                  log,
		errorfHandler:        errorfHandler,
		panicHandler:         panicHandler,
	}
}

func (tc *SimpleTestContext) Errorf(format string, args ...any) {
	tc.log.Error(fmt.Sprintf(format, args...))
	if tc.errorfHandler != nil {
		tc.errorfHandler(format, args...)
	}
}

func (*SimpleTestContext) FailNow() {
	panic(failNowMessage)
}

// RecoverAndExit is intended to be deferred by the caller to ensure
// cleanup functions are called before exit or re-panic (in the event
// of an unexpected panic or a panic during cleanup).
func (tc *SimpleTestContext) RecoverAndExit() {
	// Only exit non-zero if a cleanup caused a panic
	exitNonZero := false

	var panicData any
	if r := recover(); r != nil {
		errorString, ok := r.(string)
		if !ok || errorString != failNowMessage {
			tc.log.Error("unexpected panic",
				zap.Any("panic", r),
			)
			if tc.panicHandler != nil {
				tc.panicHandler(r)
			}
			// Retain the panic data to raise after cleanup
			panicData = r
		} else {
			// Ensure a non-zero exit due to an assertion failure
			exitNonZero = true
		}
	}

	if panicDuringCleanup := tc.cleanup(); panicDuringCleanup {
		exitNonZero = true
	}

	if panicData != nil {
		// Re-throw an unexpected (non-assertion) panic
		panic(panicData)
	}
	if exitNonZero {
		os.Exit(1)
	}
}

// Recover is intended to be deferred in a function executing a test whose
// assertions may result in panics. Such a panic is intended to be recovered to
// allow cleanup functions to be called before execution continues.
func (tc *SimpleTestContext) Recover() {
	tc.recover(false /* rethrow */)
}

// RecoverAndRethrow is intended to be deferred in a function executing a test
// whose assertions may result in panics.  Such a panic is intended to be recovered
// to allow cleanup functions to be called before the panic is rethrown.
func (tc *SimpleTestContext) RecoverAndRethrow() {
	tc.recover(true /* rethrow */)
}

// Recover is intended to be deferred in a function executing a test
// whose assertions may result in panics. Such a panic is intended to
// be recovered to allow cleanup functions to be called. A panic can
// be optionally rethrown by setting `rethrow` to true.
func (tc *SimpleTestContext) recover(rethrow bool) {
	// Recover from test failure
	var panicData any
	if panicData = recover(); panicData != nil {
		errorString, ok := panicData.(string)
		if !ok || errorString != failNowMessage {
			tc.log.Error("unexpected panic",
				zap.Any("panic", panicData),
			)
			if tc.panicHandler != nil {
				tc.panicHandler(panicData)
			}
		}
	}
	// Ensure cleanup functions are called
	_ = tc.cleanup()

	if rethrow && panicData != nil {
		panic(panicData)
	}
}

// cleanup ensures that the registered cleanup functions have been
// called. Cleanup functions will be called at most once. Returns a
// boolean indication of whether a panic results from executing one or
// more cleanup functions i.e. to trigger a non-zero exit.
func (tc *SimpleTestContext) cleanup() bool {
	if tc.cleanupCalled {
		return false
	}
	tc.cleanupCalled = true

	panicDuringCleanup := false
	for _, cleanupFunc := range tc.cleanupFuncs {
		func() {
			// Ensure a failed cleanup doesn't prevent subsequent cleanup functions from running
			defer func() {
				if r := recover(); r != nil {
					panicDuringCleanup = true
					tc.log.Error("recovered from panic during cleanup",
						zap.Any("panic", r),
					)
				}
			}()
			cleanupFunc()
		}()
	}
	return panicDuringCleanup
}

func (tc *SimpleTestContext) DeferCleanup(cleanup func()) {
	tc.cleanupFuncs = append(tc.cleanupFuncs, cleanup)
}

func (tc *SimpleTestContext) By(msg string, callback ...func()) {
	tc.log.Info("Step: " + msg)

	if len(callback) == 1 {
		callback[0]()
	} else if len(callback) > 1 {
		tc.Errorf("just one callback per By, please")
		tc.FailNow()
	}
}

func (tc *SimpleTestContext) Log() logging.Logger {
	return tc.log
}

// Helper simplifying use of a timed context by canceling the context on ginkgo teardown.
func (tc *SimpleTestContext) ContextWithTimeout(duration time.Duration) context.Context {
	return ContextWithTimeout(tc, duration)
}

// Helper simplifying use of a timed context configured with the default timeout.
func (tc *SimpleTestContext) DefaultContext() context.Context {
	return DefaultContext(tc)
}

// Helper simplifying use via an option of a timed context configured with the default timeout.
func (tc *SimpleTestContext) WithDefaultContext() common.Option {
	return WithDefaultContext(tc)
}

func (tc *SimpleTestContext) GetDefaultContextParent() context.Context {
	return tc.defaultContextParent
}

func (tc *SimpleTestContext) SetDefaultContextParent(parent context.Context) {
	tc.defaultContextParent = parent
}

func (tc *SimpleTestContext) Eventually(condition func() bool, waitFor time.Duration, tick time.Duration, msg string) {
	require.Eventually(tc, condition, waitFor, tick, msg)
}
