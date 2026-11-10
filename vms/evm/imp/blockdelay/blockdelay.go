// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package blockdelay enforces and paces block building on the ACP-226 minimum
// block delay, specified here:
// https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/226-dynamic-minimum-block-times/README.md
package blockdelay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/graft/evm/acp226"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/evm/imp/dynamic"

	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

// ErrBelowMinimum is returned by [Next] for a block built before the minimum
// block delay since its parent has elapsed.
var ErrBelowMinimum = errors.New("block time below the ACP-226 minimum block delay")

// Exponent returns the delay exponent stored in a header's ACP-226
// MinDelayExcess field, defaulting to [dynamic.InitialDelayExponent] for headers
// that predate the field.
func Exponent(excess *acp226.DelayExcess) dynamic.DelayExponent {
	if excess == nil {
		return dynamic.InitialDelayExponent
	}
	return dynamic.DelayExponent(*excess)
}

// Earliest returns the earliest time at which a child of a block at parentTime
// with delay exponent parent may be built.
func Earliest(parentTime time.Time, parent dynamic.DelayExponent) time.Time {
	return parentTime.Add(parent.DelayDuration())
}

// Next returns the delay exponent of a block built at childTime, moved from its
// parent's exponent toward desired (nil = no move). It returns
// [ErrBelowMinimum] if childTime is before [Earliest].
func Next(parentTime time.Time, parent dynamic.DelayExponent, childTime time.Time, desired *dynamic.DelayExponent) (dynamic.DelayExponent, error) {
	if minTime := Earliest(parentTime, parent); childTime.Before(minTime) {
		return 0, fmt.Errorf("%w: block time %s is before the minimum %s (parent %s + %s)",
			ErrBelowMinimum, childTime, minTime, parentTime, parent.DelayDuration())
	}
	return parent.Toward(desired), nil
}

// MinEventSpacing is the minimum spacing between consecutive successful
// [Pacer.WaitForEvent] returns. 100ms isn't special here, it was selected as a
// reasonable frequency for the engine to poll on whether to build a block or
// not.
const MinEventSpacing = 100 * time.Millisecond

// A Pacer spaces block-building events on the minimum block delay and
// [MinEventSpacing]. The spacing is a temporary workaround for txpools that
// re-signal pending txs while their block is processing.
type Pacer struct {
	now  func() time.Time
	last utils.Atomic[time.Time]
}

// NewPacer returns a [Pacer] reading the current time from now.
func NewPacer(now func() time.Time) *Pacer {
	return &Pacer{now: now}
}

// WaitForEvent waits until both earliest and [MinEventSpacing] after the
// previous successful return have passed, then returns the result of wait. If
// ctx is canceled first, it returns the cancellation cause without calling
// wait.
func (p *Pacer) WaitForEvent(ctx context.Context, earliest time.Time, wait func(context.Context) (snowcommon.Message, error)) (snowcommon.Message, error) {
	// Throttle to avoid busy looping: the txpools only clear after block
	// execution, so pending txs can re-signal while their block is processing.
	//
	// TODO(JonathanOppenheimer): The txpools should track preference / reorgs
	// so we don't need this throttle.
	until := p.last.Get().Add(MinEventSpacing)
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

	msg, err := wait(ctx)
	if err == nil {
		p.last.Set(p.now())
	}
	return msg, err
}
