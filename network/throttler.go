package network

import (
	"context"
	"golang.org/x/time/rate"
	"math/rand"
	"time"
)

type backoffPolicy interface {
	backoff(attempt int)
}

type staticBackoffPolicy struct {
	backoffDuration time.Duration
}

func (p staticBackoffPolicy) getBackoffDuration() time.Duration {
	return p.backoffDuration
}

func (p staticBackoffPolicy) backoff(_ int) {
	time.Sleep(p.getBackoffDuration())
}

type incrementalBackoffPolicy struct {
	backoffDuration   time.Duration
	incrementDuration time.Duration
}

func (n incrementalBackoffPolicy) getBackoffDuration(attempt int) time.Duration {
	incrementDurationMillis := n.getIncrementDuration().Milliseconds()
	backoffDurationMillis := n.backoffDuration.Milliseconds()
	sleepMillis := backoffDurationMillis + (incrementDurationMillis * int64(attempt))
	return time.Duration(sleepMillis) * time.Millisecond
}

func (n incrementalBackoffPolicy) getIncrementDuration() time.Duration {
	return n.incrementDuration
}

func (n incrementalBackoffPolicy) backoff(attempt int) {
	time.Sleep(n.getBackoffDuration(attempt))
}

type randomisedBackoffPolicy struct {
	minDuration time.Duration
	maxDuration time.Duration
}

// getBackoffDuration If this function is called outside of the `backoff` method, its value
// (randomised) is not the one to be used when the actual Backoff happens since the Backoff method
// calls this internally.
func (r randomisedBackoffPolicy) getBackoffDuration() time.Duration {
	randMillis := rand.Float64() * float64(r.maxDuration-r.minDuration)
	return r.minDuration + time.Duration(randMillis)
}

func (r randomisedBackoffPolicy) backoff(_ int) {
	time.Sleep(r.getBackoffDuration())
}

type Throttler interface {
	Acquire() error
}

type waitingThrottler struct {
	limiter *rate.Limiter
}

type backoffThrottler struct {
	limiter       *rate.Limiter
	backoffPolicy backoffPolicy
}

func (w waitingThrottler) Acquire() error {
	return w.limiter.Wait(context.Background())
}

func (t backoffThrottler) Acquire() error {
	attempt := 0
	for {
		if t.limiter.Allow() {
			break
		}

		t.backoffPolicy.backoff(attempt)
		attempt += 1
	}

	return nil
}

func NewBackoffThrottler(throttleLimit int, backoffPolicy backoffPolicy) Throttler {
	return backoffThrottler{
		limiter:       rate.NewLimiter(rate.Limit(throttleLimit), throttleLimit),
		backoffPolicy: backoffPolicy,
	}
}

func NewWaitingThrottler(throttleLimit int) Throttler {
	return waitingThrottler{
		limiter: rate.NewLimiter(rate.Limit(throttleLimit), throttleLimit),
	}
}

func NewStaticBackoffThrottler(throttleLimit int, backOffDuration time.Duration) Throttler {
	return backoffThrottler{
		limiter:       rate.NewLimiter(rate.Limit(throttleLimit), throttleLimit),
		backoffPolicy: staticBackoffPolicy{backoffDuration: backOffDuration},
	}
}

func NewIncrementalBackoffThrottler(throttleLimit int, backOffDuration time.Duration, incrementDuration time.Duration) Throttler {
	return backoffThrottler{
		limiter:       rate.NewLimiter(rate.Limit(throttleLimit), throttleLimit),
		backoffPolicy: incrementalBackoffPolicy{backoffDuration: backOffDuration, incrementDuration: incrementDuration},
	}
}

func NewRandomisedBackoffThrottler(throttleLimit int, minDuration, maxDuration time.Duration) Throttler {
	return backoffThrottler{
		limiter: rate.NewLimiter(rate.Limit(throttleLimit), throttleLimit),
		backoffPolicy: randomisedBackoffPolicy{
			minDuration: minDuration,
			maxDuration: maxDuration,
		},
	}
}
