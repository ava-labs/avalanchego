// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dialer

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/chain4travel/caminogo/network/throttling"
	"github.com/chain4travel/caminogo/utils"
	"github.com/chain4travel/caminogo/utils/logging"
)

var _ Dialer = &dialer{}

// Dialer attempts to create a connection with the provided IP/port pair
type Dialer interface {
	// If [ctx] is canceled, gives up trying to connect to [ip]
	// and returns an error.
	Dial(ctx context.Context, ip utils.IPDesc) (net.Conn, error)
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
		"dialer has outgoing connection limit of %d/second and dial timeout %s",
		dialerConfig.ThrottleRps,
		dialerConfig.ConnectionTimeout,
	)
	return &dialer{
		dialer:    net.Dialer{Timeout: dialerConfig.ConnectionTimeout},
		log:       log,
		network:   network,
		throttler: throttler,
	}
}

func (d *dialer) Dial(ctx context.Context, ip utils.IPDesc) (net.Conn, error) {
	if err := d.throttler.Acquire(ctx); err != nil {
		return nil, err
	}
	d.log.Verbo("dialing %s", ip)
	conn, err := d.dialer.DialContext(ctx, d.network, ip.String())
	if err != nil {
		return nil, fmt.Errorf("error while dialing %s: %w", ip, err)
	}
	return conn, nil
}
