package network

import (
	"context"

	"golang.org/x/time/rate"
)

var (
	_ DialThrottler = &dialThrottler{}
	_ DialThrottler = &noDialThrottler{}
)

type DialThrottler interface {

	// Block until the event associated with this Acquire can happen.
	// If [ctx] is canceled, gives up and returns an error.
	Acquire(ctx context.Context) error
}

type dialThrottler struct {
	limiter *rate.Limiter
}

type noDialThrottler struct{}

func (t dialThrottler) Acquire(ctx context.Context) error {
	return t.limiter.Wait(ctx)
}

func NewDialThrottler(throttleLimit int) DialThrottler {
	return dialThrottler{
		limiter: rate.NewLimiter(rate.Limit(throttleLimit), throttleLimit),
	}
}

func NewNoDialThrottler() DialThrottler {
	return noDialThrottler{}
}

func (t noDialThrottler) Acquire(context.Context) error {
	return nil
}
