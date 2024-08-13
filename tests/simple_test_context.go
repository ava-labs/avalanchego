// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/onsi/ginkgo/v2/formatter"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

const failNowMessage = "SimpleTestContext.FailNow called"

type SimpleTestContext struct {
	cleanupFuncs  []func()
	cleanupCalled bool
}

func NewTestContext() *SimpleTestContext {
	return &SimpleTestContext{}
}

func (*SimpleTestContext) Errorf(format string, args ...interface{}) {
	log.Printf("error: "+format, args...)
}

func (*SimpleTestContext) FailNow() {
	panic(failNowMessage)
}

func (*SimpleTestContext) GetWriter() io.Writer {
	return os.Stdout
}

// Cleanup is intended to be deferred by the caller to ensure cleanup is performed even
// in the event that a panic occurs.
func (tc *SimpleTestContext) Cleanup() {
	if tc.cleanupCalled {
		return
	}
	tc.cleanupCalled = true

	// Only exit non-zero if a cleanup caused a panic
	exitNonZero := false

	var panicData any
	if r := recover(); r != nil {
		errorString, ok := r.(string)
		if !ok || errorString != failNowMessage {
			// Retain the panic data to raise after cleanup
			panicData = r
		} else {
			exitNonZero = true
		}
	}

	for _, cleanupFunc := range tc.cleanupFuncs {
		func() {
			// Ensure a failed cleanup doesn't prevent subsequent cleanup functions from running
			defer func() {
				if r := recover(); r != nil {
					exitNonZero = true
					fmt.Println("Recovered from panic during cleanup:", r)
				}
			}()
			cleanupFunc()
		}()
	}

	if panicData != nil {
		panic(panicData)
	}
	if exitNonZero {
		os.Exit(1)
	}
}

func (tc *SimpleTestContext) DeferCleanup(cleanup func()) {
	tc.cleanupFuncs = append(tc.cleanupFuncs, cleanup)
}

func (tc *SimpleTestContext) By(_ string, _ ...func()) {
	tc.Errorf("By not yet implemented")
	tc.FailNow()
}

// TODO(marun) Enable color output equivalent to GinkgoTestContext.Outf
func (*SimpleTestContext) Outf(format string, args ...interface{}) {
	s := formatter.F(format, args...)
	log.Print(s)
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

func (tc *SimpleTestContext) Eventually(condition func() bool, waitFor time.Duration, tick time.Duration, msg string) {
	require.Eventually(tc, condition, waitFor, tick, msg)
}
