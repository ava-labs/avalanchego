// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamicip

import (
	"context"
	"errors"
	"net"
)

const openDNSUrl = "resolver1.opendns.com:53"

var (
	errOpenDNSNoIP = errors.New("openDNS returned no ip")

	_ Resolver = (*openDNSResolver)(nil)
)

// IFConfigResolves resolves our public IP using openDNS
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

func (r *openDNSResolver) Resolve(ctx context.Context) (net.IP, error) {
	ips, err := r.resolver.LookupIP(ctx, "ip", "myip.opendns.com")
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, errOpenDNSNoIP
	}
	return ips[0], nil
}
