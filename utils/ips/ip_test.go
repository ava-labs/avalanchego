// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ips

import (
	"encoding/json"
	"net"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIPPortEqual(t *testing.T) {
	tests := []struct {
		ipPort  string
		ipPort1 IPPort
		ipPort2 IPPort
		result  bool
	}{
		// Expected equal
		{
			`"127.0.0.1:0"`,
			IPPort{net.ParseIP("127.0.0.1"), 0},
			IPPort{net.ParseIP("127.0.0.1"), 0},
			true,
		},
		{
			`"[::1]:0"`,
			IPPort{net.ParseIP("::1"), 0},
			IPPort{net.ParseIP("::1"), 0},
			true,
		},
		{
			`"127.0.0.1:0"`,
			IPPort{net.ParseIP("127.0.0.1"), 0},
			IPPort{net.ParseIP("::ffff:127.0.0.1"), 0},
			true,
		},

		// Expected unequal
		{
			`"127.0.0.1:0"`,
			IPPort{net.ParseIP("127.0.0.1"), 0},
			IPPort{net.ParseIP("1.2.3.4"), 0},
			false,
		},
		{
			`"[::1]:0"`,
			IPPort{net.ParseIP("::1"), 0},
			IPPort{net.ParseIP("2001::1"), 0},
			false,
		},
		{
			`"127.0.0.1:0"`,
			IPPort{net.ParseIP("127.0.0.1"), 0},
			IPPort{net.ParseIP("127.0.0.1"), 1},
			false,
		},
	}
	for i, tt := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			require := require.New(t)

			ipPort := IPDesc{}
			require.NoError(ipPort.UnmarshalJSON([]byte(tt.ipPort)))
			require.Equal(tt.ipPort1, IPPort(ipPort))

			ipPortJSON, err := json.Marshal(ipPort)
			require.NoError(err)
			require.Equal(tt.ipPort, string(ipPortJSON))

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
			require.Equal(t, tt.result, tt.ipPort.String())
		})
	}
}

func TestToIPPortError(t *testing.T) {
	tests := []struct {
		in          string
		out         IPPort
		expectedErr error
	}{
		{
			in:          "",
			out:         IPPort{},
			expectedErr: errBadIP,
		},
		{
			in:          ":",
			out:         IPPort{},
			expectedErr: strconv.ErrSyntax,
		},
		{
			in:          "abc:",
			out:         IPPort{},
			expectedErr: strconv.ErrSyntax,
		},
		{
			in:          ":abc",
			out:         IPPort{},
			expectedErr: strconv.ErrSyntax,
		},
		{
			in:          "abc:abc",
			out:         IPPort{},
			expectedErr: strconv.ErrSyntax,
		},
		{
			in:          "127.0.0.1:",
			out:         IPPort{},
			expectedErr: strconv.ErrSyntax,
		},
		{
			in:          ":1",
			out:         IPPort{},
			expectedErr: errBadIP,
		},
		{
			in:          "::1",
			out:         IPPort{},
			expectedErr: errBadIP,
		},
		{
			in:          "::1:42",
			out:         IPPort{},
			expectedErr: errBadIP,
		},
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
			require.Equal(tt.out, result)
		})
	}
}
