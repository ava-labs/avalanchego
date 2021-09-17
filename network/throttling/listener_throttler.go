// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttling

import (
	"context"
	"net"

	"golang.org/x/time/rate"
)

var _ net.Listener = &throttledListener{}

// Wraps [listener] and returns a net.Listener that will accept
// at most [maxConnsPerSec] connections per second.
func NewThrottledListener(listener net.Listener, maxConnsPerSec int) net.Listener {
	return &throttledListener{
		listener: listener,
		limiter:  rate.NewLimiter(rate.Limit(maxConnsPerSec), maxConnsPerSec),
	}
}

// [throttledListener] is a net.Listener that rate-limits
// acceptance of incoming connections.
type throttledListener struct {
	// The underlying listener
	listener net.Listener
	// Handles rate-limiting
	limiter *rate.Limiter
}

func (l *throttledListener) Accept() (net.Conn, error) {
	// Wait until the rate-limiter says to accept the
	// next incoming connection
	if err := l.limiter.Wait(context.Background()); err != nil {
		return nil, err
	}
	return l.listener.Accept()
}

func (l *throttledListener) Close() error {
	return l.listener.Close()
}

func (l *throttledListener) Addr() net.Addr {
	return l.listener.Addr()
}
