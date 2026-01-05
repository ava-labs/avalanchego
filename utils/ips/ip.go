// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ips

import "net/netip"

// IsPublic returns true if the provided address is considered to be a public
// IP.
func IsPublic(addr netip.Addr) bool {
	return addr.IsGlobalUnicast() && !addr.IsPrivate()
}

// ParseAddr returns the IP address from the provided string. If the string
// represents an IPv4 address in an IPv6 address, the IPv4 address is returned.
func ParseAddr(s string) (netip.Addr, error) {
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, err
	}
	if addr.Is4In6() {
		addr = addr.Unmap()
	}
	return addr, nil
}

// ParseAddrPort returns the IP:port address from the provided string. If the
// string represents an IPv4 address in an IPv6 address, the IPv4 address is
// returned.
func ParseAddrPort(s string) (netip.AddrPort, error) {
	addrPort, err := netip.ParseAddrPort(s)
	if err != nil {
		return netip.AddrPort{}, err
	}
	addr := addrPort.Addr()
	if addr.Is4In6() {
		addrPort = netip.AddrPortFrom(
			addr.Unmap(),
			addrPort.Port(),
		)
	}
	return addrPort, nil
}

// AddrFromSlice returns the IP address from the provided byte slice. If the
// byte slice represents an IPv4 address in an IPv6 address, the IPv4 address is
// returned.
func AddrFromSlice(b []byte) (netip.Addr, bool) {
	addr, ok := netip.AddrFromSlice(b)
	if !ok {
		return netip.Addr{}, false
	}
	if addr.Is4In6() {
		addr = addr.Unmap()
	}
	return addr, true
}
