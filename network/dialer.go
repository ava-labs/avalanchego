// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/ava-labs/avalanchego/utils"
)

var dialTimeout = 30 * time.Second

// Dialer attempts to create a connection with the provided IP/port pair
type Dialer interface {
	// If [ctx] is cancelled, gives up trying to connect to [ip]
	// and returns an error.
	Dial(ctx context.Context, ip utils.IPDesc) (net.Conn, error)
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

func (d *dialer) Dial(ctx context.Context, ip utils.IPDesc) (net.Conn, error) {
	if err := d.throttler.Acquire(ctx); err != nil {
		return nil, err
	}
	dialer := net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, d.network, ip.String())
	if err != nil {
		return nil, fmt.Errorf("error while dialing %s: %s", ip, err)
	}
	return conn, nil
}
