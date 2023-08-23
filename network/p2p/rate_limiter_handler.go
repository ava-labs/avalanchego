// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/time/rate"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	_                    Handler = (*RateLimiterHandler)(nil)
	ErrRateLimitExceeded         = errors.New("rate limit exceeded")
)

type RateLimiterHandler struct {
	Handler
	RateLimiter *rate.Limiter
}

func (r RateLimiterHandler) AppGossip(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte) error {
	if !r.RateLimiter.Allow() {
		return fmt.Errorf("dropping app gossip message from %s: %w", nodeID, ErrRateLimitExceeded)
	}

	return r.Handler.AppGossip(ctx, nodeID, gossipBytes)
}

func (r RateLimiterHandler) AppRequest(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, error) {
	if !r.RateLimiter.Allow() {
		return nil, fmt.Errorf("dropping app request from %s: %w", nodeID, ErrRateLimitExceeded)
	}

	return r.Handler.AppRequest(ctx, nodeID, deadline, requestBytes)
}

func (r RateLimiterHandler) CrossChainAppRequest(ctx context.Context, chainID ids.ID, deadline time.Time, requestBytes []byte) ([]byte, error) {
	if !r.RateLimiter.Allow() {
		return nil, fmt.Errorf("dropping cross-chain app request from %s: %w", chainID, ErrRateLimitExceeded)
	}

	return r.Handler.CrossChainAppRequest(ctx, chainID, deadline, requestBytes)
}
