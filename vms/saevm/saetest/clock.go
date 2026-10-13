// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saetest

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
	"github.com/ava-labs/avalanchego/vms/saevm/params"
)

// Clock is a mutable, test-controlled clock used to drive the block-building and
// consensus time of a [github.com/ava-labs/avalanchego/vms/saevm/sae.VM] and the
// hooks layered on it. It is safe for concurrent use, so a test goroutine may
// advance it while the VM reads it.
type Clock struct {
	// settleResolution is fixed at construction and never mutated, so it needs
	// no locking.
	settleResolution time.Duration

	mu  sync.Mutex
	now time.Time
}

// SettlingBlock is the subset of
// [github.com/ava-labs/avalanchego/vms/saevm/blocks.Block] that
// [Clock.AdvanceToSettle] needs. It is an interface so this package avoids
// importing blocks, whose internal tests import this package.
type SettlingBlock interface {
	WaitUntilExecuted(context.Context) error
	ExecutedByGasTime() *gastime.Time
}

// NewClock returns a [Clock] fixed at startTime. settleResolution is the
// resolution at which the hooks under test record block timestamps:
// [Clock.AdvanceToSettle] rounds its target up to a multiple of it so the next
// block's timestamp still reaches the settlement threshold once the hooks
// truncate it. Pass [time.Nanosecond] for hooks that preserve full resolution.
func NewClock(startTime time.Time, settleResolution time.Duration) *Clock {
	return &Clock{settleResolution: settleResolution, now: startTime}
}

// Now returns the current time and is suitable as a VM/hook "now" function.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Set fixes the clock at t.
func (c *Clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// Advance moves the clock forward by d.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// AdvanceToSettle advances the clock such that the next call to [Clock.Now] is
// at or after the time required to settle b. Note that at least one more
// accepted block is still required to actually settle b.
func (c *Clock) AdvanceToSettle(ctx context.Context, tb testing.TB, b SettlingBlock) {
	tb.Helper()
	require.NoErrorf(tb, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
	to := b.ExecutedByGasTime().AsTime().Add(params.Tau + c.settleResolution - time.Nanosecond).Truncate(c.settleResolution)

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.now.Before(to) {
		c.now = to
	}
}
