// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"encoding/json"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAtomic(t *testing.T) {
	require := require.New(t)

	var a Atomic[bool]
	require.Zero(a.Get())

	a.Set(false)
	require.False(a.Get())

	a.Set(true)
	require.True(a.Get())

	a.Set(false)
	require.False(a.Get())
}

func TestAtomicJSON(t *testing.T) {
	tests := []struct {
		name     string
		value    *Atomic[netip.AddrPort]
		expected string
	}{
		{
			name:     "zero value",
			value:    new(Atomic[netip.AddrPort]),
			expected: `""`,
		},
		{
			name: "ipv4 value",
			value: NewAtomic(netip.AddrPortFrom(
				netip.AddrFrom4([4]byte{1, 2, 3, 4}),
				12345,
			)),
			expected: `"1.2.3.4:12345"`,
		},
		{
			name: "ipv6 loopback",
			value: NewAtomic(netip.AddrPortFrom(
				netip.IPv6Loopback(),
				12345,
			)),
			expected: `"[::1]:12345"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			b, err := json.Marshal(test.value)
			require.NoError(err)
			require.Equal(test.expected, string(b))

			var parsed Atomic[netip.AddrPort]
			require.NoError(json.Unmarshal([]byte(test.expected), &parsed))
			require.Equal(test.value.Get(), parsed.Get())
		})
	}
}
