// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamicip

import (
	"context"
	"errors"
	"net"
	"net/netip"

	"github.com/ava-labs/avalanchego/utils/ips"
)

const openDNSUrl = "resolver1.opendns.com:53"

var (
	errOpenDNSNoIP = errors.New("openDNS returned no ip")

	_ Resolver = (*openDNSResolver)(nil)
)

// openDNSResolver resolves our public IP using openDNS
type openDNSResolver struct {
	resolver *net.Resolver
}

func newOpenDNSResolver() Resolver {
	return &openDNSResolver{
		resolver: &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
				d := net.Dialer{}
				return d.DialContext(ctx, "udp", openDNSUrl)
			},
		},
	}
}

func (r *openDNSResolver) Resolve(ctx context.Context) (netip.Addr, error) {
	resolvedIPs, err := r.resolver.LookupIP(ctx, "ip", "myip.opendns.com")
	if err != nil {
		return netip.Addr{}, err
	}
	for _, ip := range resolvedIPs {
		if addr, ok := ips.AddrFromSlice(ip); ok {
			return addr, nil
		}
	}
	return netip.Addr{}, errOpenDNSNoIP
}
