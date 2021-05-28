// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"fmt"
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

// NewDialer returns a new Dialer that calls `net.Dial` with the provided
// network.
func NewDialer(network string, throttleAps uint32, minBackoff, maxBackoff time.Duration) Dialer {
	if throttleAps <= 0 {
		throttleAps = math.MaxInt32
	}
	return &dialer{network: network, throttler: NewRandomisedBackoffThrottler(int(throttleAps), minBackoff, maxBackoff)}
}

func (d *dialer) Dial(ip utils.IPDesc) (net.Conn, error) {
	fmt.Println(time.Now(), "Acquiring lock to dial", ip)
	d.throttler.Acquire()
	fmt.Println(time.Now(), "Acquired lock, dialing", ip)
	return net.Dial(d.network, ip.String())
}
