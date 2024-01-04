// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ips

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLookup(t *testing.T) {
	tests := []struct {
		host string
		ip   net.IP
	}{
		{
			host: "127.0.0.1",
			ip:   net.ParseIP("127.0.0.1").To4(),
		},
		{
			host: "localhost",
			ip:   net.ParseIP("127.0.0.1").To4(),
		},
		{
			host: "::",
			ip:   net.IPv6zero,
		},
		{
			host: "0.0.0.0",
			ip:   net.ParseIP("0.0.0.0").To4(),
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
