// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

// A long default timeout used to timeout failed operations but unlikely to induce
// flaking due to unexpected resource contention.
const DefaultTimeout = 2 * time.Minute

// Helper simplifying use of a timed context by canceling the context with the test context.
func ContextWithTimeout(tc TestContext, duration time.Duration) context.Context {
	parent := tc.GetDefaultContextParent()
	ctx, cancel := context.WithTimeout(parent, duration)
	tc.DeferCleanup(cancel)
	return ctx
}

// Helper simplifying use of a timed context configured with the default timeout.
func DefaultContext(tc TestContext) context.Context {
	return ContextWithTimeout(tc, DefaultTimeout)
}

// Helper simplifying use via an option of a timed context configured with the default timeout.
func WithDefaultContext(tc TestContext) common.Option {
	return common.WithContext(DefaultContext(tc))
}
