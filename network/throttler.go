package network

import (
	"context"
	"golang.org/x/time/rate"
	"math/rand"
	"time"
)

type BackoffPolicy interface {
	Backoff(attempt int)
}

type StaticBackoffPolicy struct {
	backoffDuration time.Duration
}

func (p StaticBackoffPolicy) GetBackoffDuration() time.Duration {
	return p.backoffDuration
}

func (p StaticBackoffPolicy) Backoff(_ int) {
	time.Sleep(p.GetBackoffDuration())
}

type IncrementalBackoffPolicy struct {
	backoffDuration   time.Duration
	incrementDuration time.Duration
}

func (n IncrementalBackoffPolicy) GetBackoffDuration(attempt int) time.Duration {
	incrementDurationMillis := n.GetIncrementDuration().Milliseconds()
	backoffDurationMillis := n.backoffDuration.Milliseconds()
	sleepMillis := backoffDurationMillis + (incrementDurationMillis * int64(attempt))
	return time.Duration(sleepMillis) * time.Millisecond
}

func (n IncrementalBackoffPolicy) GetIncrementDuration() time.Duration {
	return n.incrementDuration
}

func (n IncrementalBackoffPolicy) Backoff(attempt int) {
	time.Sleep(n.GetBackoffDuration(attempt))
}

type RandomisedBackoffPolicy struct {
	minDuration time.Duration
	maxDuration time.Duration
}

// GetBackoffDuration If this function is called outside of the `Backoff` method, its value
// (randomised) is not the one to be used when the actual Backoff happens since the Backoff method
// calls this internally.
func (r RandomisedBackoffPolicy) GetBackoffDuration() time.Duration {
	randMillis := rand.Float64() * float64(r.maxDuration-r.minDuration)
	return r.minDuration + time.Duration(randMillis)
}

func (r RandomisedBackoffPolicy) Backoff(_ int) {
	time.Sleep(r.GetBackoffDuration())
}

type Throttler interface {
	Acquire()
}

type WaitingThrottler struct {
	limiter *rate.Limiter
}

type BackoffThrottler struct {
	limiter       *rate.Limiter
	backoffPolicy BackoffPolicy
}

func (w WaitingThrottler) Acquire() {
	_ = w.limiter.Wait(context.Background())
}

func (t BackoffThrottler) Acquire() {
	attempt := 0
	for {
		if t.limiter.Allow() {
			break
		}

		t.backoffPolicy.Backoff(attempt)
		attempt += 1
	}
}

func NewBackoffThrottler(throttleLimit int, backoffPolicy BackoffPolicy) Throttler {
	return BackoffThrottler{
		limiter:       rate.NewLimiter(rate.Limit(throttleLimit), throttleLimit),
		backoffPolicy: backoffPolicy,
	}
}

func NewWaitingThrottler(throttleLimit int) Throttler {
	return WaitingThrottler{
		limiter: rate.NewLimiter(rate.Limit(throttleLimit), throttleLimit),
	}
}

func NewStaticBackoffThrottler(throttleLimit int, backOffDuration time.Duration) Throttler {
	return BackoffThrottler{
		limiter:       rate.NewLimiter(rate.Limit(throttleLimit), throttleLimit),
		backoffPolicy: StaticBackoffPolicy{backoffDuration: backOffDuration},
	}
}

func NewIncrementalBackoffThrottler(throttleLimit int, backOffDuration time.Duration, incrementDuration time.Duration) Throttler {
	return BackoffThrottler{
		limiter:       rate.NewLimiter(rate.Limit(throttleLimit), throttleLimit),
		backoffPolicy: IncrementalBackoffPolicy{backoffDuration: backOffDuration, incrementDuration: incrementDuration},
	}
}

func NewRandomisedBackoffThrottler(throttleLimit int, minDuration, maxDuration time.Duration) Throttler {
	return BackoffThrottler{
		limiter: rate.NewLimiter(rate.Limit(throttleLimit), throttleLimit),
		backoffPolicy: RandomisedBackoffPolicy{
			minDuration: minDuration,
			maxDuration: maxDuration,
		},
	}
}
