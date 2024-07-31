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

const failNowMessage = "OtherTestContext.FailNow called"

// TODO(marun) Consider a more descriptive name for this type
type OtherTestContext struct {
	cleanupFuncs  []func()
	cleanupCalled bool
}

func NewTestContext() *OtherTestContext {
	return &OtherTestContext{}
}

func (*OtherTestContext) Errorf(format string, args ...interface{}) {
	log.Printf("error: "+format, args...)
}

func (*OtherTestContext) FailNow() {
	panic(failNowMessage)
}

func (*OtherTestContext) GetWriter() io.Writer {
	return os.Stdout
}

// Cleanup is intended to be deferred by the caller to ensure cleanup is performed even
// in the event that a panic occurs.
func (tc *OtherTestContext) Cleanup() {
	if tc.cleanupCalled {
		return
	}
	tc.cleanupCalled = true

	// Only exit non-zero if a panic caused cleanup or cleanup caused a panic
	exitNonZero := false

	if r := recover(); r != nil {
		exitNonZero = true
		if r.(string) != failNowMessage {
			// Only print panic messages not originating from this context
			fmt.Println(r)
		}
	}

	for _, cleanupFunc := range tc.cleanupFuncs {
		func() {
			// Ensure a failed cleanup doesn't prevent subsequent cleanup functions from running
			defer func() {
				if r := recover(); r != nil {
					exitNonZero = true
					fmt.Println("Recovered from in panic during cleanup:", r)
				}
			}()
			cleanupFunc()
		}()
	}

	if exitNonZero {
		os.Exit(1)
	}
}

func (tc *OtherTestContext) DeferCleanup(cleanup func()) {
	tc.cleanupFuncs = append(tc.cleanupFuncs, cleanup)
}

func (tc *OtherTestContext) By(_ string, _ ...func()) {
	tc.Errorf("By not yet implemented")
	tc.FailNow()
}

// TODO(marun) Enable color output equivalent to GinkgoTestContext.Outf
func (*OtherTestContext) Outf(format string, args ...interface{}) {
	s := formatter.F(format, args...)
	log.Print(s)
}

// Helper simplifying use of a timed context by canceling the context on ginkgo teardown.
func (tc *OtherTestContext) ContextWithTimeout(duration time.Duration) context.Context {
	return ContextWithTimeout(tc, duration)
}

// Helper simplifying use of a timed context configured with the default timeout.
func (tc *OtherTestContext) DefaultContext() context.Context {
	return DefaultContext(tc)
}

// Helper simplifying use via an option of a timed context configured with the default timeout.
func (tc *OtherTestContext) WithDefaultContext() common.Option {
	return WithDefaultContext(tc)
}

func (tc *OtherTestContext) Eventually(condition func() bool, waitFor time.Duration, tick time.Duration, msg string) {
	require.Eventually(tc, condition, waitFor, tick, msg)
}
