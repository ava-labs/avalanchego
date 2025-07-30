// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dialer

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ Dialer = (*dialer)(nil)

// Dialer attempts to create a connection with the provided IP/port pair
type Dialer interface {
	// If [ctx] is canceled, gives up trying to connect to [ip]
	// and returns an error.
	Dial(ctx context.Context, ip netip.AddrPort) (net.Conn, error)
}

type dialer struct {
	dialer    net.Dialer
	log       logging.Logger
	network   string
	throttler throttling.DialThrottler
}

type Config struct {
	ThrottleRps       uint32        `json:"throttleRps"`
	ConnectionTimeout time.Duration `json:"connectionTimeout"`
}

// NewDialer returns a new Dialer that calls net.Dial with the provided network.
// [network] is the network passed into Dial. Should probably be "TCP".
// [dialerConfig.connectionTimeout] gives the timeout when dialing an IP.
// [dialerConfig.throttleRps] gives the max number of outgoing connection attempts/second.
// If [dialerConfig.throttleRps] == 0, outgoing connections aren't rate-limited.
func NewDialer(network string, dialerConfig Config, log logging.Logger) Dialer {
	var throttler throttling.DialThrottler
	if dialerConfig.ThrottleRps <= 0 {
		throttler = throttling.NewNoDialThrottler()
	} else {
		throttler = throttling.NewDialThrottler(int(dialerConfig.ThrottleRps))
	}
	log.Debug(
		"creating dialer",
		zap.Uint32("throttleRPS", dialerConfig.ThrottleRps),
		zap.Duration("dialTimeout", dialerConfig.ConnectionTimeout),
	)
	return &dialer{
		dialer:    net.Dialer{Timeout: dialerConfig.ConnectionTimeout},
		log:       log,
		network:   network,
		throttler: throttler,
	}
}

func (d *dialer) Dial(ctx context.Context, ip netip.AddrPort) (net.Conn, error) {
	if err := d.throttler.Acquire(ctx); err != nil {
		return nil, err
	}
	d.log.Verbo("dialing",
		zap.Stringer("ip", ip),
	)
	conn, err := d.dialer.DialContext(ctx, d.network, ip.String())
	if err != nil {
		return nil, fmt.Errorf("error while dialing %s: %w", ip, err)
	}
	return conn, nil
}
