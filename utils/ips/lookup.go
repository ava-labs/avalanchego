// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ips

import (
	"context"
	"errors"
	"net"
	"net/netip"
)

var errNoIPsFound = errors.New("no IPs found")

// Lookup attempts to resolve a hostname to a single IP. If multiple IPs are
// found, then lookup will attempt to return an IPv4 address, otherwise it will
// pick any of the IPs.
//
// Note: IPv4 is preferred because `net.Listen` prefers IPv4.
func Lookup(ctx context.Context, hostname string) (netip.Addr, error) {
	ips, err := (&net.Resolver{}).LookupIPAddr(ctx, hostname)
	if err != nil {
		return netip.Addr{}, err
	}
	if len(ips) == 0 {
		return netip.Addr{}, errNoIPsFound
	}

	for _, ip := range ips {
		ipv4 := ip.IP.To4()
		if ipv4 != nil {
			addr, _ := AddrFromSlice(ipv4)
			return addr, nil
		}
	}
	addr, _ := AddrFromSlice(ips[0].IP)
	return addr, nil
}
