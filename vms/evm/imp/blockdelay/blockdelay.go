// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package blockdelay paces block building on the ACP-226 minimum block delay,
// specified here:
// https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/226-dynamic-minimum-block-times/README.md
package blockdelay

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/graft/evm/acp226"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/evm/imp/dynamic"

	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

// Exponent returns the delay exponent stored in a header's ACP-226
// MinDelayExcess field, defaulting to [dynamic.InitialDelayExponent] for headers
// that predate the field.
//
// TODO(JonathanOppenheimer): Replace with a generic pointer-dereference helper
// once the acp226 package is removed.
func Exponent(excess *acp226.DelayExcess) dynamic.DelayExponent {
	if excess == nil {
		return dynamic.InitialDelayExponent
	}
	return dynamic.DelayExponent(*excess)
}

// minEventSpacing is the minimum spacing between consecutive successful
// [Pacer.WaitForEvent] returns. 100ms isn't special here, it was selected as a
// reasonable frequency for the engine to poll on whether to build a block or
// not.
const minEventSpacing = 100 * time.Millisecond

// An EventWaiter blocks until there is an event for the consensus engine.
type EventWaiter interface {
	WaitForEvent(context.Context) (snowcommon.Message, error)
}

// A Pacer wraps an [EventWaiter], delaying each wait until the minimum block
// delay has elapsed and a minimum spacing since the previous event has passed.
type Pacer struct {
	now    func() time.Time
	waiter EventWaiter
	last   utils.Atomic[time.Time]
}

// NewPacer returns a [Pacer] that wraps waiter and reads the current time from
// now.
func NewPacer(now func() time.Time, waiter EventWaiter) *Pacer {
	return &Pacer{now: now, waiter: waiter}
}

// WaitForEvent waits until both earliest and a minimum spacing after the
// previous successful return have passed, then returns the result of the
// wrapped [EventWaiter]. If ctx is canceled first, it returns the cancellation
// cause without calling the wrapped [EventWaiter].
func (p *Pacer) WaitForEvent(ctx context.Context, earliest time.Time) (snowcommon.Message, error) {
	// Throttle to avoid busy looping: the SAE mempool and the C-Chain's
	// cross-chain txpool only drop txs once their block executes, so they keep
	// reporting them as pending while the block is processing.
	//
	// TODO(JonathanOppenheimer): Those txpools should track the preferred
	// block, including reorgs, so we don't need this throttle.
	until := p.last.Get().Add(minEventSpacing)
	if earliest.After(until) {
		until = earliest
	}
	if d := until.Sub(p.now()); d > 0 {
		select {
		case <-ctx.Done():
			return 0, context.Cause(ctx)
		case <-time.After(d):
		}
	}

	msg, err := p.waiter.WaitForEvent(ctx)
	if err == nil {
		p.last.Set(p.now())
	}
	return msg, err
}
