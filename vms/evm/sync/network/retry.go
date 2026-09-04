// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/ava-labs/libevm/libevm/options"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// RetryOption overrides a retry setting on [NewDispatcher].
type RetryOption = options.Option[retryPolicy]

func WithPeerFailureBackoff(d time.Duration) RetryOption {
	return options.Func[retryPolicy](func(p *retryPolicy) { p.peerFailureBackoff = d })
}

func WithNoPeersInitialBackoff(d time.Duration) RetryOption {
	return options.Func[retryPolicy](func(p *retryPolicy) { p.noPeersInitialBackoff = d })
}

func WithNoPeersMaxBackoff(d time.Duration) RetryOption {
	return options.Func[retryPolicy](func(p *retryPolicy) { p.noPeersMaxBackoff = d })
}

func WithNoPeersFactor(f float64) RetryOption {
	return options.Func[retryPolicy](func(p *retryPolicy) { p.noPeersFactor = f })
}

// retryPolicy paces retries: a flat delay for peer-scoped failures, an
// escalating one when no peer is available. Configure it only via [RetryOption].
type retryPolicy struct {
	// Flat wait after a peer-scoped or semantic failure.
	peerFailureBackoff time.Duration
	// Capped-exponential wait keyed on consecutive no-peer attempts.
	noPeersInitialBackoff time.Duration
	noPeersMaxBackoff     time.Duration
	noPeersFactor         float64
}

func defaultRetryPolicy() *retryPolicy {
	return &retryPolicy{
		peerFailureBackoff:    10 * time.Millisecond,
		noPeersInitialBackoff: 10 * time.Millisecond,
		noPeersMaxBackoff:     time.Second,
		noPeersFactor:         1.5,
	}
}

// noPeersBackoff escalates the wait per no-peer attempt so we stop busy-polling
// an empty network.
func (p retryPolicy) noPeersBackoff(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	// Each failed attempt waits factor times longer, up to the cap.
	d := float64(p.noPeersInitialBackoff) * math.Pow(p.noPeersFactor, float64(attempt))
	if d >= float64(p.noPeersMaxBackoff) {
		return p.noPeersMaxBackoff
	}
	return time.Duration(d)
}

type retryClass int

const (
	retryFatal      retryClass = iota // retrying cannot help
	retryNoPeers                      // no peer available, the escalating case
	retryPeerScoped                   // this peer's fault, de-score it and try another
)

func classify(err error) retryClass {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), errors.Is(err, errMarshalRequest):
		return retryFatal
	case errors.Is(err, errNoPeers):
		return retryNoPeers
	default:
		return retryPeerScoped
	}
}

// doRetry retries attempt until verify accepts a response, ctx ends, or a fatal
// error. attempt must return a fresh response each call so failures never merge.
func doRetry[Resp proto.Message](
	ctx context.Context,
	log logging.Logger,
	policy retryPolicy,
	verify func(Resp, ids.NodeID) error,
	attempt func() (Resp, ids.NodeID, *Outcome, error),
) (Resp, error) {
	var (
		zero           Resp
		attempts       int
		lastErr        error
		noPeerAttempts int
	)
	for {
		if err := ctx.Err(); err != nil {
			return zero, retryFailure(err, lastErr, attempts)
		}

		attempts++
		resp, nodeID, outcome, err := attempt()
		var wait time.Duration
		if err == nil {
			// verify reports its own rejection, since only the caller knows what
			// made the response wrong.
			verifyErr := verify(resp, nodeID)
			if verifyErr == nil {
				outcome.Success()
				return resp, nil
			}
			outcome.Failure()
			lastErr = verifyErr
			noPeerAttempts = 0
			wait = policy.peerFailureBackoff
		} else {
			switch classify(err) {
			case retryFatal:
				return zero, retryFailure(err, lastErr, attempts)
			case retryNoPeers:
				log.Debug("no peer available, retrying", zap.Error(err))
				lastErr = err
				wait = policy.noPeersBackoff(noPeerAttempts)
				noPeerAttempts++
			default:
				log.Debug("request failed, retrying",
					zap.Stringer("nodeID", nodeID),
					zap.Error(err),
				)
				lastErr = err
				noPeerAttempts = 0
				wait = policy.peerFailureBackoff
			}
		}
		if err := backoff(ctx, wait); err != nil {
			return zero, retryFailure(err, lastErr, attempts)
		}
	}
}

// retryFailure pairs the error that ended the loop with the one that kept it
// retrying, so an expired ctx names a cause.
func retryFailure(err, lastErr error, attempts int) error {
	if lastErr == nil || errors.Is(err, lastErr) {
		return err
	}
	return fmt.Errorf("%w after %d attempts, last failure: %w", err, attempts, lastErr)
}

// backoff waits d or until ctx ends, returning the ctx error if it ends first.
func backoff(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
