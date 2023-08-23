// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"sync"

	"golang.org/x/time/rate"

	"github.com/ava-labs/avalanchego/ids"
)

var _ Throttler = (*TokenBucketThrottler)(nil)

type Throttler interface {
	// Throttle blocks until there is [tokens] capacity for [nodeID]
	Throttle(ctx context.Context, nodeID ids.NodeID, tokens int) error
}

func NewTokenBucketThrottler(refill rate.Limit, burst int) *TokenBucketThrottler {
	return &TokenBucketThrottler{
		refill:       refill,
		burst:        burst,
		tokenBuckets: make(map[ids.NodeID]*rate.Limiter),
	}
}

type TokenBucketThrottler struct {
	refill rate.Limit
	burst  int

	lock         sync.Mutex
	tokenBuckets map[ids.NodeID]*rate.Limiter
}

func (t *TokenBucketThrottler) Throttle(ctx context.Context, nodeID ids.NodeID, tokens int) error {
	t.lock.Lock()
	tokenBucket, ok := t.tokenBuckets[nodeID]
	if !ok {
		tokenBucket = rate.NewLimiter(t.refill, t.burst)
		t.tokenBuckets[nodeID] = tokenBucket
	}
	t.lock.Unlock()

	return tokenBucket.WaitN(ctx, tokens)
}
