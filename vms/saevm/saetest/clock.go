// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saetest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/saevm/gastime"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
)

// Clock is a mutable, test-controlled clock used to drive the block-building and
// consensus time of a [sae.VM] and the hooks layered on it.
type Clock struct {
	time.Time
	settleResolution time.Duration
}

// SettlingBlock is the subset of [blocks.Block] that [Clock.AdvanceToSettle]
// needs. It is an interface so this package avoids importing blocks, whose
// internal tests import this package.
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
	return &Clock{Time: startTime, settleResolution: settleResolution}
}

// Now returns the current time and is suitable as a VM/hook "now" function.
func (c *Clock) Now() time.Time { return c.Time }

// Set fixes the clock at t.
func (c *Clock) Set(t time.Time) { c.Time = t }

// Advance moves the clock forward by d.
func (c *Clock) Advance(d time.Duration) { c.Time = c.Time.Add(d) }

// AdvanceToSettle advances the clock such that the next call to [Clock.Now] is
// at or after the time required to settle b. Note that at least one more
// accepted block is still required to actually settle b.
func (c *Clock) AdvanceToSettle(ctx context.Context, tb testing.TB, b SettlingBlock) {
	tb.Helper()
	require.NoErrorf(tb, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
	to := b.ExecutedByGasTime().AsTime().Add(saeparams.Tau + c.settleResolution - time.Nanosecond).Truncate(c.settleResolution)
	if c.Before(to) {
		c.Set(to)
	}
}
