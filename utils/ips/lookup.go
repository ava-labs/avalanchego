// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ips

import (
	"errors"
	"net"
)

var errNoIPsFound = errors.New("no IPs found")

// Lookup attempts to resolve a hostname to a single IP. If multiple IPs are
// found, then lookup will attempt to return an IPv4 address, otherwise it will
// pick any of the IPs.
//
// Note: IPv4 is preferred because `net.Listen` prefers IPv4.
func Lookup(hostname string) (net.IP, error) {
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, errNoIPsFound
	}

	for _, ip := range ips {
		ipv4 := ip.To4()
		if ipv4 != nil {
			return ipv4, nil
		}
	}
	return ips[0], nil
}
