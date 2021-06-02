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

// Dialer attempts to create a connection with the provided IP/port pair
type Dialer interface {
	// If [ctx] is canceled, gives up trying to connect to [ip]
	// and returns an error.
	Dial(ctx context.Context, ip utils.IPDesc) (net.Conn, error)
}

type dialer struct {
	network           string
	throttler         Throttler
	connectionTimeout time.Duration
}

type DialerConfig struct {
	throttleRps       uint32
	connectionTimeout time.Duration
}

func NewDialerConfig(throttleRps uint32, dialTimeout time.Duration) DialerConfig {
	return DialerConfig{
		throttleRps,
		dialTimeout,
	}
}

// NewDialer returns a new Dialer that calls `net.Dial` with the provided
// network.
func NewDialer(network string, dialerConfig DialerConfig) Dialer {
	var throttler Throttler
	if dialerConfig.throttleRps <= 0 {
		fmt.Println("Throttling is turned off")
		throttler = NewNoThrottler()
	} else {
		fmt.Printf("Throttling %d rps\n", dialerConfig.throttleRps)
		throttler = NewThrottler(int(dialerConfig.throttleRps))
	}

	fmt.Println("Connection timeout", dialerConfig.connectionTimeout)

	return &dialer{
		network:           network,
		throttler:         throttler,
		connectionTimeout: dialerConfig.connectionTimeout,
	}
}

func (d *dialer) Dial(ctx context.Context, ip utils.IPDesc) (net.Conn, error) {
	if err := d.throttler.Acquire(ctx); err != nil {
		return nil, err
	}
	dialer := net.Dialer{Timeout: d.connectionTimeout}
	conn, err := dialer.DialContext(ctx, d.network, ip.String())
	if err != nil {
		return nil, fmt.Errorf("error while dialing %s: %s", ip, err)
	}
	return conn, nil
}
