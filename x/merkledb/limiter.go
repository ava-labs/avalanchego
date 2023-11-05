package merkledb

import (
	"context"
	"sync/atomic"
)

type limiter struct {
	current atomic.Int32
}

func newLimiter(limit int32) *limiter {
	result := &limiter{current: atomic.Int32{}}
	result.current.Store(limit)
	return result
}

func (l *limiter) acquire(ctx context.Context) bool {
	var ctxDoneCh <-chan struct{}
	if ctx != nil {
		ctxDoneCh = ctx.Done()
	}

	for {
		select {
		case <-ctxDoneCh:
			return false
		default:
		}
		current := l.current.Load()
		if l.current.CompareAndSwap(current, current-1) {
			return true
		}
	}
}

func (l *limiter) tryAcquire() bool {
	for {
		current := l.current.Load()
		if current == 0 {
			return false
		}
		if l.current.CompareAndSwap(current, current-1) {
			return true
		}
	}
}

func (l *limiter) release() {
	for {
		current := l.current.Load()
		if l.current.CompareAndSwap(current, current+1) {
			return
		}
	}
}
