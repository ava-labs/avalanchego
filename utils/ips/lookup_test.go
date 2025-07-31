// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ips

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLookup(t *testing.T) {
	tests := []struct {
		host string
		ip   netip.Addr
	}{
		{
			host: "127.0.0.1",
			ip:   netip.AddrFrom4([4]byte{127, 0, 0, 1}),
		},
		{
			host: "localhost",
			ip:   netip.AddrFrom4([4]byte{127, 0, 0, 1}),
		},
		{
			host: "::",
			ip:   netip.IPv6Unspecified(),
		},
		{
			host: "0.0.0.0",
			ip:   netip.IPv4Unspecified(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			require := require.New(t)

			ip, err := Lookup(tt.host)
			require.NoError(err)
			require.Equal(tt.ip, ip)
		})
	}
}
