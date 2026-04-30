// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"time"
)

// minBucketInterval is the smallest tick the limiter uses regardless of
// target TPS. At very high TPS the natural per-tx interval (1s/TPS) drops
// below scheduler accuracy, so we coalesce releases into 5 ms buckets.
const minBucketInterval = 5 * time.Millisecond

// tpsLimiter is a token-bucket rate limiter that releases tokens in batches
// at a ticker cadence chosen to match the target TPS. At low TPS the bucket
// interval matches the per-tx period (one token per tick); at high TPS the
// interval is held at minBucketInterval and many tokens are released per
// tick to avoid waking a goroutine thousands of times per second.
//
// A nil receiver is treated as "unlimited" — Wait returns immediately.
type tpsLimiter struct {
	bucket   chan struct{}
	interval time.Duration
}

// newTPSLimiter returns nil for tps <= 0 (unlimited). Otherwise it returns a
// limiter whose bucket releases tokens at the requested rate. Run must be
// invoked once on a goroutine before Wait is called.
func newTPSLimiter(tps int) *tpsLimiter {
	if tps <= 0 {
		return nil
	}
	interval := time.Second / time.Duration(tps)
	if interval < minBucketInterval {
		interval = minBucketInterval
	}
	tokensPerTick := int(float64(tps) * interval.Seconds())
	if tokensPerTick < 1 {
		tokensPerTick = 1
	}
	return &tpsLimiter{
		bucket:   make(chan struct{}, tokensPerTick),
		interval: interval,
	}
}

// Run pumps tokens into the bucket until ctx is cancelled. Excess tokens
// (when consumers haven't drained the previous tick's tokens yet) are
// dropped; this caps the burst at one bucket-worth of tokens.
func (l *tpsLimiter) Run(ctx context.Context) {
	ticker := time.NewTicker(l.interval)
	defer ticker.Stop()
	tokensPerTick := cap(l.bucket)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for i := 0; i < tokensPerTick; i++ {
				select {
				case l.bucket <- struct{}{}:
				default:
				}
			}
		}
	}
}

// Wait blocks until a token is available or ctx is done. A nil limiter
// returns immediately.
func (l *tpsLimiter) Wait(ctx context.Context) error {
	if l == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-l.bucket:
		return nil
	}
}
