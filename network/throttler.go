package network

import (
	"context"

	"golang.org/x/time/rate"
)

var (
	_ Throttler = &throttler{}
	_ Throttler = &noThrottler{}
)

type Throttler interface {
	// Block until the event associated with this Acquire can happen.
	// If [ctx] is canceled, gives up and returns an error.
	Acquire(ctx context.Context) error
}

type throttler struct {
	limiter *rate.Limiter
}

type noThrottler struct{}

func (t throttler) Acquire(ctx context.Context) error {
	return t.limiter.Wait(ctx)
}

func NewThrottler(throttleLimit int) Throttler {
	return throttler{
		limiter: rate.NewLimiter(rate.Limit(throttleLimit), throttleLimit),
	}
}

func NewNoThrottler() Throttler {
	return noThrottler{}
}

func (t noThrottler) Acquire(context.Context) error {
	return nil
}
