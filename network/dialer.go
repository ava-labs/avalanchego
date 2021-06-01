// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"github.com/ava-labs/avalanchego/utils"
	"net"
	"time"
)

// Dialer attempts to create a connection with the provided IP/port pair
type Dialer interface {
	Dial(ctx context.Context, ip utils.IPDesc) (<-chan net.Conn, <-chan error, error)
}

type dialer struct {
	network   string
	throttler Throttler
}

type DialerConfig struct {
	throttleAps uint32
	minBackoff  time.Duration
	maxBackoff  time.Duration
}

func NewDialerConfig(throttleAps uint32, minBackoff, maxBackoff time.Duration) DialerConfig {
	return DialerConfig{
		throttleAps,
		minBackoff,
		maxBackoff,
	}
}

// NewDialer returns a new Dialer that calls `net.Dial` with the provided
// network.
func NewDialer(network string, dialerConfig DialerConfig) Dialer {
	var throttler Throttler
	if dialerConfig.throttleAps <= 0 {
		throttler = NewNoThrottler()
	} else {
		throttler = NewWaitingThrottler(int(dialerConfig.throttleAps))
	}

	return &dialer{
		network:   network,
		throttler: throttler,
	}
}

func (d *dialer) Dial(ctx context.Context, ip utils.IPDesc) (<-chan net.Conn, <-chan error, error) {
	cch := make(chan net.Conn, 1)
	ech := make(chan error, 1)

	go d.dial(ctx, ip, cch, ech)

	return cch, ech, nil
}

func (d dialer) dial(ctx context.Context, ip utils.IPDesc, cch chan net.Conn, ech chan error) {
	err := d.throttler.Acquire(ctx)

	if err != nil {
		close(cch)
		ech <- err
		close(ech)
		return
	}

	conn, err := net.Dial(d.network, ip.String())

	if err != nil {
		close(cch)
		ech <- err
		close(ech)
		return
	}

	cch <- conn
	close(cch)
	close(ech)
}
