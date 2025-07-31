// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"encoding/json"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProcessContext(t *testing.T) {
	tests := []struct {
		name     string
		context  ProcessContext
		expected string
	}{
		{
			name: "ipv4 loopback",
			context: ProcessContext{
				PID: 1,
				URI: "http://localhost:9650",
				StakingAddress: netip.AddrPortFrom(
					netip.AddrFrom4([4]byte{127, 0, 0, 1}),
					9651,
				),
			},
			expected: `{
	"pid": 1,
	"uri": "http://localhost:9650",
	"stakingAddress": "127.0.0.1:9651"
}`,
		},
		{
			name: "ipv6 loopback",
			context: ProcessContext{
				PID: 1,
				URI: "http://localhost:9650",
				StakingAddress: netip.AddrPortFrom(
					netip.IPv6Loopback(),
					9651,
				),
			},
			expected: `{
	"pid": 1,
	"uri": "http://localhost:9650",
	"stakingAddress": "[::1]:9651"
}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			contextJSON, err := json.MarshalIndent(test.context, "", "\t")
			require.NoError(err)
			require.JSONEq(test.expected, string(contextJSON))
		})
	}
}
