// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmsync

import "sync/atomic"

// pivot encapsulates the logic for deciding when to forward
// a new sync target based on a fixed block-height interval. It is
// safe for concurrent use.
type pivot struct {
	interval uint64
	// nextHeight is the next height threshold at or beyond which we
	// should forward an update. A value of 0 means uninitialized.
	nextHeight uint64 // accessed atomically
}

func newPivotPolicy(interval uint64) *pivot {
	return &pivot{interval: interval}
}

// shouldForward reports whether a summary at the given height should be
// forwarded, initializing the next threshold on first use. When it returns
// true, callers should follow up with advance().
func (p *pivot) shouldForward(height uint64) bool {
	if p == nil || p.interval == 0 {
		return true
	}
	next := atomic.LoadUint64(&p.nextHeight)
	if next == 0 {
		// Round up the initial height to the next multiple of interval.
		// Ceil division: ((h + interval - 1) / interval) * interval
		h := height
		init := ((h + p.interval - 1) / p.interval) * p.interval
		// Initialize once - if another goroutine wins, read the established value.
		if !atomic.CompareAndSwapUint64(&p.nextHeight, 0, init) {
			next = atomic.LoadUint64(&p.nextHeight)
		} else {
			next = init
		}
	}
	return height >= next
}

// advance moves the next threshold forward by one interval. Call this
// only after shouldForward has returned true and the update was issued.
func (p *pivot) advance() {
	if p == nil || p.interval == 0 {
		return
	}
	atomic.AddUint64(&p.nextHeight, p.interval)
}
