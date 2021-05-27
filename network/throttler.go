package network

import (
	"golang.org/x/time/rate"
	"math"
	"math/rand"
	"time"
)

type Throttler interface {
	Acquire()
}

type BlockingThrottler struct {
	limiter   *rate.Limiter
	onBackOff func(int)
}

func (t BlockingThrottler) Acquire() {
	attempt := 0
	for {
		if t.limiter.Allow() {
			break
		}

		attempt += 1
		t.onBackOff(attempt)
	}
}

func NewThrottler(throttleLimit int, onBackOffFn func(int)) Throttler {
	return BlockingThrottler{
		limiter:   rate.NewLimiter(rate.Limit(throttleLimit), throttleLimit),
		onBackOff: onBackOffFn,
	}
}

func NewStaticBackoffThrottler(throttleLimit int, backOffDuration time.Duration) Throttler {
	return BlockingThrottler{
		limiter:   rate.NewLimiter(rate.Limit(throttleLimit), throttleLimit),
		onBackOff: staticBackoffFn(backOffDuration),
	}
}

func staticBackoffFn(backOffDuration time.Duration) func(attempt int) {
	return func(_ int) {
		time.Sleep(backOffDuration)
	}
}

func NewIncrementalBackoffThrottler(throttleLimit int, backOffDuration time.Duration, incrementDuration time.Duration) Throttler {
	return BlockingThrottler{
		limiter:   rate.NewLimiter(rate.Limit(throttleLimit), throttleLimit),
		onBackOff: incrementalBackoffFn(backOffDuration, incrementDuration),
	}
}

func incrementalBackoffFn(backOffDuration time.Duration, incrementDuration time.Duration) func(attempt int) {
	return func(attempt int) {
		sleepMillis := float64(backOffDuration.Milliseconds()) + math.Pow(float64(incrementDuration), float64(attempt))
		time.Sleep(time.Duration(sleepMillis) * time.Millisecond)
	}
}

func NewRandomisedBackoffThrottler(throttleLimit int, minDuration, maxDuration time.Duration) Throttler {
	return BlockingThrottler{
		limiter:   rate.NewLimiter(rate.Limit(throttleLimit), throttleLimit),
		onBackOff: randomisedBackoffFn(maxDuration, minDuration),
	}
}

func randomisedBackoffFn(maxDuration time.Duration, minDuration time.Duration) func(attempt int) {
	return func(attempt int) {
		randMillis := (rand.Int63() * (maxDuration.Milliseconds() - minDuration.Milliseconds())) + minDuration.Milliseconds()
		time.Sleep(time.Duration(randMillis) * time.Millisecond)
	}
}
