package network

import (
	"golang.org/x/time/rate"
	"sync"
	"time"
)

type Throttler struct {
	limiter *rate.Limiter
	lock    sync.Mutex
}

func NewThrottler(throttleLimit int) Throttler {
	return Throttler{
		limiter: rate.NewLimiter(rate.Limit(throttleLimit), throttleLimit),
		lock:    sync.Mutex{},
	}
}

func (t *Throttler) Acquire() {
	t.lock.Lock()
	defer t.lock.Unlock()
	for {
		if t.limiter.Allow() {
			break
		}
		time.Sleep(1 * time.Second)
	}
}
