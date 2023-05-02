// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ips

import (
	"fmt"
	"net"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIPPortEqual(t *testing.T) {
	tests := []struct {
		ipPort1 IPPort
		ipPort2 IPPort
		result  bool
	}{
		// Expected equal
		{
			IPPort{net.ParseIP("127.0.0.1"), 0},
			IPPort{net.ParseIP("127.0.0.1"), 0},
			true,
		},
		{
			IPPort{net.ParseIP("::1"), 0},
			IPPort{net.ParseIP("::1"), 0},
			true,
		},
		{
			IPPort{net.ParseIP("127.0.0.1"), 0},
			IPPort{net.ParseIP("::ffff:127.0.0.1"), 0},
			true,
		},

		// Expected unequal
		{
			IPPort{net.ParseIP("127.0.0.1"), 0},
			IPPort{net.ParseIP("1.2.3.4"), 0},
			false,
		},
		{
			IPPort{net.ParseIP("::1"), 0},
			IPPort{net.ParseIP("2001::1"), 0},
			false,
		},
		{
			IPPort{net.ParseIP("127.0.0.1"), 0},
			IPPort{net.ParseIP("127.0.0.1"), 1},
			false,
		},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			require := require.New(t)

			require.NotNil(tt.ipPort1.IP)
			require.NotNil(tt.ipPort2.IP)

			require.Equal(tt.result, tt.ipPort1.Equal(tt.ipPort2))
		})
	}
}

func TestIPPortString(t *testing.T) {
	tests := []struct {
		ipPort IPPort
		result string
	}{
		{IPPort{net.ParseIP("127.0.0.1"), 0}, "127.0.0.1:0"},
		{IPPort{net.ParseIP("::1"), 42}, "[::1]:42"},
		{IPPort{net.ParseIP("::ffff:127.0.0.1"), 65535}, "127.0.0.1:65535"},
		{IPPort{net.IP{}, 1234}, "<nil>:1234"},
	}
	for _, tt := range tests {
		t.Run(tt.result, func(t *testing.T) {
			require := require.New(t)

			require.Equal(tt.result, tt.ipPort.String())
		})
	}
}

func TestToIPPortError(t *testing.T) {
	tests := []struct {
		in          string
		out         IPPort
		expectedErr error
	}{
		{"", IPPort{}, errBadIP},
		{":", IPPort{}, strconv.ErrSyntax},
		{"abc:", IPPort{}, strconv.ErrSyntax},
		{":abc", IPPort{}, strconv.ErrSyntax},
		{"abc:abc", IPPort{}, strconv.ErrSyntax},
		{"127.0.0.1:", IPPort{}, strconv.ErrSyntax},
		{":1", IPPort{}, errBadIP},
		{"::1", IPPort{}, errBadIP},
		{"::1:42", IPPort{}, errBadIP},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			require := require.New(t)

			result, err := ToIPPort(tt.in)
			require.ErrorIs(err, tt.expectedErr)
			require.Equal(tt.out, result)
		})
	}
}

func TestToIPPort(t *testing.T) {
	tests := []struct {
		in  string
		out IPPort
	}{
		{"127.0.0.1:42", IPPort{net.ParseIP("127.0.0.1"), 42}},
		{"[::1]:42", IPPort{net.ParseIP("::1"), 42}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			require := require.New(t)

			result, err := ToIPPort(tt.in)
			require.NoError(err)
			require.Equal(result, tt.out)
		})
	}
}
