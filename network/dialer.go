// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ Dialer = &dialer{}

// Dialer attempts to create a connection with the provided IP/port pair
type Dialer interface {
	// If [ctx] is canceled, gives up trying to connect to [ip]
	// and returns an error.
	Dial(ctx context.Context, ip utils.IPDesc) (net.Conn, error)
}

type dialer struct {
	log               logging.Logger
	network           string
	throttler         DialThrottler
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

// NewDialer returns a new Dialer that calls net.Dial with the provided network.
// [network] is the network passed into Dial. Should probably be "TCP".
// [dialerConfig.connectionTimeout] gives the timeout when dialing an IP.
// [dialerConfig.throttleRps] gives the max number of outgoing connection attempts/second.
// If [dialerConfig.throttleRps] == 0, outgoing connections aren't rate-limited.
func NewDialer(network string, dialerConfig DialerConfig, log logging.Logger) Dialer {
	var throttler DialThrottler
	if dialerConfig.throttleRps <= 0 {
		throttler = NewNoDialThrottler()
	} else {
		throttler = NewDialThrottler(int(dialerConfig.throttleRps))
	}
	log.Debug(
		"dialer has outgoing connection limit of %d/second and dial timeout %s",
		dialerConfig.throttleRps,
		dialerConfig.connectionTimeout,
	)
	return &dialer{
		log:               log,
		network:           network,
		throttler:         throttler,
		connectionTimeout: dialerConfig.connectionTimeout,
	}
}

func (d *dialer) Dial(ctx context.Context, ip utils.IPDesc) (net.Conn, error) {
	if err := d.throttler.Acquire(ctx); err != nil {
		return nil, err
	}
	d.log.Verbo("dialing %s", ip)
	dialer := net.Dialer{Timeout: d.connectionTimeout}
	conn, err := dialer.DialContext(ctx, d.network, ip.String())
	if err != nil {
		return nil, fmt.Errorf("error while dialing %s: %s", ip, err)
	}
	return conn, nil
}
