// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nat

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"time"
)

var (
	_ Router = (*noRouter)(nil)

	errNoRouterCantMapPorts = errors.New("can't map ports without a known router")
	errFetchingIP           = errors.New("getting outbound IP failed")
)

const googleDNSServer = "8.8.8.8:80"

type noRouter struct {
	ip    netip.Addr
	ipErr error
}

func (noRouter) SupportsNAT() bool {
	return false
}

func (noRouter) MapPort(uint16, uint16, string, time.Duration) error {
	return errNoRouterCantMapPorts
}

func (noRouter) UnmapPort(uint16, uint16) error {
	return nil
}

func (r noRouter) ExternalIP() (netip.Addr, error) {
	return r.ip, r.ipErr
}

func getOutboundIP() (netip.Addr, error) {
	conn, err := (&net.Dialer{}).DialContext(context.Background(), "udp", googleDNSServer)
	if err != nil {
		return netip.Addr{}, err
	}

	localAddr := conn.LocalAddr()
	if err := conn.Close(); err != nil {
		return netip.Addr{}, err
	}

	udpAddr, ok := localAddr.(*net.UDPAddr)
	if !ok {
		return netip.Addr{}, errFetchingIP
	}
	addr := udpAddr.AddrPort().Addr()
	if addr.Is4In6() {
		addr = addr.Unmap()
	}
	return addr, nil
}

// NewNoRouter returns a router that assumes the network is public
func NewNoRouter() Router {
	ip, err := getOutboundIP()
	return &noRouter{
		ip:    ip,
		ipErr: err,
	}
}
