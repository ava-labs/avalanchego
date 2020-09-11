// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"net"

	"github.com/ava-labs/avalanchego/utils"
)

// Dialer attempts to create a connection with the provided IP/port pair
type Dialer interface {
	Dial(utils.IPDesc) (net.Conn, error)
}

type dialer struct {
	network string
}

// NewDialer returns a new Dialer that calls `net.Dial` with the provided
// network.
func NewDialer(network string) Dialer { return &dialer{network: network} }

func (d *dialer) Dial(ip utils.IPDesc) (net.Conn, error) { return net.Dial(d.network, ip.String()) }
