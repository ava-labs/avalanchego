// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ips

import "net/netip"

// IsPublic returns true if the provided address is considered to be a public
// IP.
func IsPublic(addr netip.Addr) bool {
	return addr.IsGlobalUnicast() && !addr.IsPrivate()
}

// FromSlice returns the IP address from the provided byte slice. If the byte
// slice represents an IPv4 address in an IPv6 address, the IPv4 address is
// returned.
func FromSlice(b []byte) (netip.Addr, bool) {
	addr, ok := netip.AddrFromSlice(b)
	if !ok {
		return netip.Addr{}, false
	}
	if addr.Is4In6() {
		addr = addr.Unmap()
	}
	return addr, true
}
