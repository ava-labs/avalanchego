// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"github.com/ava-labs/avalanchego/utils"
	"math"
	"net"
	"time"
)

// Dialer attempts to create a connection with the provided IP/port pair
type Dialer interface {
	Dial(utils.IPDesc) (net.Conn, error)
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
	if dialerConfig.throttleAps <= 0 {
		dialerConfig.throttleAps = math.MaxInt32
	}
	return &dialer{network: network, throttler: NewRandomisedBackoffThrottler(int(dialerConfig.throttleAps), dialerConfig.minBackoff, dialerConfig.maxBackoff)}
}

func (d *dialer) Dial(ip utils.IPDesc) (net.Conn, error) {
	d.throttler.Acquire()
	return net.Dial(d.network, ip.String())
}
