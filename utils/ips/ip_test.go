// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ips

import (
	"fmt"
	"net"
	"testing"
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
			if tt.ipPort1.IP == nil {
				t.Error("ipPort1 nil")
			} else if tt.ipPort2.IP == nil {
				t.Error("ipPort2 nil")
			}
			result := tt.ipPort1.Equal(tt.ipPort2)
			if result && result != tt.result {
				t.Error("Expected IPPort to be equal, but they were not")
			}
			if !result && result != tt.result {
				t.Error("Expected IPPort to be unequal, but they were equal")
			}
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
