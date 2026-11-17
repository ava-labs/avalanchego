// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package blockdelay paces block building on the [ACP-226] minimum block delay.
//
// [ACP-226]: https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/226-dynamic-minimum-block-times/README.md
package blockdelay

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/graft/evm/acp226"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/evm/imp/dynamic"

	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

// Exponent returns the delay exponent encoded by excess. A nil excess, yields
// [dynamic.InitialDelayExponent].
//
// TODO(JonathanOppenheimer): Replace with a generic pointer-dereference helper
// once the acp226 package is removed.
func Exponent(excess *acp226.DelayExcess) dynamic.DelayExponent {
	if excess == nil {
		return dynamic.InitialDelayExponent
	}
	return dynamic.DelayExponent(*excess)
}

// minEventSpacing is the minimum time between successful [Pacer.WaitForEvent]
// returns. The value is arbitrary, chosen as a reasonable interval for the
// engine to poll whether to build a block.
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

// WaitForEvent waits until earliest and the minimum spacing since the previous
// event have passed, then calls the wrapped [EventWaiter]. If ctx is canceled
// first, it returns the cancellation cause.
func (p *Pacer) WaitForEvent(ctx context.Context, earliest time.Time) (snowcommon.Message, error) {
	// Throttle to avoid busy looping. The SAE mempool and the C-Chain's
	// cross-chain txpool only drop a tx once its block executes, so they keep
	// reporting it as pending while that block is processing.
	//
	// TODO(JonathanOppenheimer): Remove this throttle once those txpools track
	// the preferred block.
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
