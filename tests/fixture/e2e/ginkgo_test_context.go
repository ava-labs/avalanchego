// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	"context"
	"io"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/formatter"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

type GinkgoTestContext struct{}

func NewTestContext() *GinkgoTestContext {
	return &GinkgoTestContext{}
}

func (*GinkgoTestContext) Errorf(format string, args ...interface{}) {
	ginkgo.GinkgoT().Errorf(format, args...)
}

func (*GinkgoTestContext) FailNow() {
	ginkgo.GinkgoT().FailNow()
}

func (*GinkgoTestContext) GetWriter() io.Writer {
	return ginkgo.GinkgoWriter
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

// Outputs to stdout.
//
// Examples:
//
//   - Out("{{green}}{{bold}}hi there %q{{/}}", "aa")
//   - Out("{{magenta}}{{bold}}hi therea{{/}} {{cyan}}{{underline}}b{{/}}")
//
// See https://github.com/onsi/ginkgo/blob/v2.0.0/formatter/formatter.go#L52-L73
// for an exhaustive list of color options.
func (*GinkgoTestContext) Outf(format string, args ...interface{}) {
	s := formatter.F(format, args...)
	// Use GinkgoWriter to ensure that output from this function is
	// printed sequentially within other test output produced with
	// GinkgoWriter (e.g. `STEP:...`) when tests are run in
	// parallel. ginkgo collects and writes stdout separately from
	// GinkgoWriter during parallel execution and the resulting output
	// can be confusing.
	ginkgo.GinkgoWriter.Print(s)
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
