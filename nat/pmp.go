// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nat

import (
	"errors"
	"math"
	"net/netip"
	"time"

	"github.com/jackpal/gateway"

	natpmp "github.com/jackpal/go-nat-pmp"
)

const (
	// pmpProtocol is intentionally lowercase and should not be confused with
	// upnpProtocol.
	// See:
	// - https://github.com/jackpal/go-nat-pmp/blob/v1.0.2/natpmp.go#L82
	pmpProtocol      = "tcp"
	pmpClientTimeout = 500 * time.Millisecond
)

var (
	_ Router = (*pmpRouter)(nil)

	errInvalidLifetime = errors.New("invalid mapping duration range")
)

// pmpRouter adapts the NAT-PMP protocol implementation so it conforms to the
// common interface.
type pmpRouter struct {
	client *natpmp.Client
}

func (*pmpRouter) SupportsNAT() bool {
	return true
}

func (r *pmpRouter) MapPort(
	newInternalPort uint16,
	newExternalPort uint16,
	_ string,
	mappingDuration time.Duration,
) error {
	internalPort := int(newInternalPort)
	externalPort := int(newExternalPort)

	// go-nat-pmp uses seconds to denote their lifetime
	lifetime := mappingDuration.Seconds()
	// Assumes the architecture is at least 32-bit
	if lifetime < 0 || lifetime > math.MaxInt32 {
		return errInvalidLifetime
	}

	_, err := r.client.AddPortMapping(pmpProtocol, internalPort, externalPort, int(lifetime))
	return err
}

func (r *pmpRouter) UnmapPort(internalPort uint16, _ uint16) error {
	internalPortInt := int(internalPort)

	_, err := r.client.AddPortMapping(pmpProtocol, internalPortInt, 0, 0)
	return err
}

func (r *pmpRouter) ExternalIP() (netip.Addr, error) {
	response, err := r.client.GetExternalAddress()
	if err != nil {
		return netip.Addr{}, err
	}
	return netip.AddrFrom4(response.ExternalIPAddress), nil
}

func getPMPRouter() *pmpRouter {
	gatewayIP, err := gateway.DiscoverGateway()
	if err != nil {
		return nil
	}

	pmp := &pmpRouter{
		client: natpmp.NewClientWithTimeout(gatewayIP, pmpClientTimeout),
	}
	if _, err := pmp.ExternalIP(); err != nil {
		return nil
	}

	return pmp
}
