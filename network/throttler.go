package network

import (
	"golang.org/x/time/rate"
	"math"
	"math/rand"
	"sync"
	"time"
)

type Throttler struct {
	limiter   *rate.Limiter
	lock      sync.Mutex
	onBackOff func(int)
}

func NewThrottler(throttleLimit int, onBackOffFn func(int)) Throttler {
	return Throttler{
		limiter:   rate.NewLimiter(rate.Limit(throttleLimit), throttleLimit),
		lock:      sync.Mutex{},
		onBackOff: onBackOffFn,
	}
}

func NewStaticBackoffThrottler(throttleLimit int, backOffDuration time.Duration) Throttler {
	return Throttler{
		limiter: rate.NewLimiter(rate.Limit(throttleLimit), throttleLimit),
		lock:    sync.Mutex{},
		onBackOff: func(attempt int) {
			time.Sleep(backOffDuration)
		},
	}
}

func NewIncrementalBackoffThrottler(throttleLimit int, backOffDuration time.Duration, incrementDuration time.Duration) Throttler {
	return Throttler{
		limiter: rate.NewLimiter(rate.Limit(throttleLimit), throttleLimit),
		lock:    sync.Mutex{},
		onBackOff: func(attempt int) {
			sleepMillis := float64(backOffDuration.Milliseconds()) + math.Pow(float64(incrementDuration), float64(attempt))
			time.Sleep(time.Duration(sleepMillis) * time.Millisecond)
		},
	}
}

func NewRandomisedBackoffThrottler(throttleLimit int, minDuration, maxDuration time.Duration) Throttler {
	return Throttler{
		limiter: rate.NewLimiter(rate.Limit(throttleLimit), throttleLimit),
		lock:    sync.Mutex{},
		onBackOff: func(attempt int) {
			randMillis := (rand.Int63() * (maxDuration.Milliseconds() - minDuration.Milliseconds())) + minDuration.Milliseconds()
			time.Sleep(time.Duration(randMillis) * time.Millisecond)
		},
	}
}

func (t *Throttler) Acquire() {
	attempt := 0
	for {
		if t.limiter.Allow() {
			break
		}

		attempt += 1
		t.onBackOff(attempt)
	}
}
