// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nat

import (
	"errors"
	"net"
	"time"
)

var (
	errNoRouterCantMapPorts        = errors.New("can't map ports without a known router")
	errFetchingIP                  = errors.New("getting outbound IP failed")
	_                       Router = &noRouter{}
)

const googleDNSServer = "8.8.8.8:80"

type noRouter struct {
	ip    net.IP
	ipErr error
}

func (noRouter) SupportsNAT() bool {
	return false
}

func (noRouter) MapPort(_ string, intPort, extPort uint16, _ string, _ time.Duration) error {
	return errNoRouterCantMapPorts
}

func (noRouter) UnmapPort(string, uint16, uint16) error {
	return nil
}

func (r noRouter) ExternalIP() (net.IP, error) {
	return r.ip, r.ipErr
}

func getOutboundIP() (net.IP, error) {
	conn, err := net.Dial("udp", googleDNSServer)
	if err != nil {
		return nil, err
	}

	addr := conn.LocalAddr()
	if err := conn.Close(); err != nil {
		return nil, err
	}

	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok {
		return nil, errFetchingIP
	}
	return udpAddr.IP, nil
}

// NewNoRouter returns a router that assumes the network is public
func NewNoRouter() Router {
	ip, err := getOutboundIP()
	return &noRouter{
		ip:    ip,
		ipErr: err,
	}
}
