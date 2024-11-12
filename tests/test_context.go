// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"context"
	"io"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

type TestContext interface {
	// Ensures the context can be used to instantiate a require instance
	require.TestingT

	// Ensures compatibility with ginkgo.By
	By(text string, callback ...func())

	// Provides a simple alternative to ginkgo.DeferCleanup
	DeferCleanup(cleanup func())

	// Enables color output to stdout
	Outf(format string, args ...interface{})

	// Ensures compatibility with ginkgo.GinkgoWriter
	GetWriter() io.Writer

	// Context helpers requiring cleanup with DeferCleanup
	ContextWithTimeout(duration time.Duration) context.Context
	DefaultContext() context.Context
	WithDefaultContext() common.Option

	// Ensures compatibility with require.Eventually
	Eventually(condition func() bool, waitFor time.Duration, tick time.Duration, msg string)
}
