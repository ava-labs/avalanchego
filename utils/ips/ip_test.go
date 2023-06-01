// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ips

import (
	"encoding/json"
	"fmt"
	"net"
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
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
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
			if result := tt.ipPort.String(); result != tt.result {
				t.Errorf("Expected %q, got %q", tt.result, result)
			}
		})
	}
}

func TestToIPPortError(t *testing.T) {
	tests := []struct {
		in  string
		out IPPort
	}{
		{"", IPPort{}},
		{":", IPPort{}},
		{"abc:", IPPort{}},
		{":abc", IPPort{}},
		{"abc:abc", IPPort{}},
		{"127.0.0.1:", IPPort{}},
		{":1", IPPort{}},
		{"::1", IPPort{}},
		{"::1:42", IPPort{}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			result, err := ToIPPort(tt.in)
			if err == nil {
				t.Errorf("Unexpected success")
			}
			if !tt.out.Equal(result) {
				t.Errorf("Expected %v, got %v", tt.out, result)
			}
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
			result, err := ToIPPort(tt.in)
			if err != nil {
				t.Errorf("Unexpected error %v", err)
			}
			if !tt.out.Equal(result) {
				t.Errorf("Expected %#v, got %#v", tt.out, result)
			}
		})
	}
}
