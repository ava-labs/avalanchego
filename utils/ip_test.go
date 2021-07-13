// (c) 2020, Alex Willmer. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"fmt"
	"net"
	"testing"
)

func TestIPDescEqual(t *testing.T) {
	tests := []struct {
		ipDesc1 IPDesc
		ipDesc2 IPDesc
		result  bool
	}{
		// Expected equal
		{
			IPDesc{net.ParseIP("127.0.0.1"), 0},
			IPDesc{net.ParseIP("127.0.0.1"), 0},
			true,
		},
		{
			IPDesc{net.ParseIP("::1"), 0},
			IPDesc{net.ParseIP("::1"), 0},
			true,
		},
		{
			IPDesc{net.ParseIP("127.0.0.1"), 0},
			IPDesc{net.ParseIP("::ffff:127.0.0.1"), 0},
			true,
		},

		// Expected unequal
		{
			IPDesc{net.ParseIP("127.0.0.1"), 0},
			IPDesc{net.ParseIP("1.2.3.4"), 0},
			false,
		},
		{
			IPDesc{net.ParseIP("::1"), 0},
			IPDesc{net.ParseIP("2001::1"), 0},
			false,
		},
		{
			IPDesc{net.ParseIP("127.0.0.1"), 0},
			IPDesc{net.ParseIP("127.0.0.1"), 1},
			false,
		},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			if tt.ipDesc1.IP == nil {
				t.Error("ipDesc1 nil")
			} else if tt.ipDesc2.IP == nil {
				t.Error("ipDesc2 nil")
			}
			result := tt.ipDesc1.Equal(tt.ipDesc2)
			if result && result != tt.result {
				t.Error("Expected IPDesc to be equal, but they were not")
			}
			if !result && result != tt.result {
				t.Error("Expected IPDesc to be unequal, but they were equal")
			}
		})
	}
}

func TestIPDescPortString(t *testing.T) {
	tests := []struct {
		ipDesc IPDesc
		result string
	}{
		{IPDesc{net.ParseIP("127.0.0.1"), 0}, ":0"},
		{IPDesc{net.ParseIP("::1"), 42}, ":42"},
		{IPDesc{net.ParseIP("::ffff:127.0.0.1"), 65535}, ":65535"},
		{IPDesc{net.IP{}, 1234}, ":1234"},
	}
	for _, tt := range tests {
		t.Run(tt.result, func(t *testing.T) {
			if result := tt.ipDesc.PortString(); result != tt.result {
				t.Errorf("Expected %q, got %q", tt.result, result)
			}
		})
	}
}

func TestIPDescString(t *testing.T) {
	tests := []struct {
		ipDesc IPDesc
		result string
	}{
		{IPDesc{net.ParseIP("127.0.0.1"), 0}, "127.0.0.1:0"},
		{IPDesc{net.ParseIP("::1"), 42}, "[::1]:42"},
		{IPDesc{net.ParseIP("::ffff:127.0.0.1"), 65535}, "127.0.0.1:65535"},
		{IPDesc{net.IP{}, 1234}, "<nil>:1234"},
	}
	for _, tt := range tests {
		t.Run(tt.result, func(t *testing.T) {
			if result := tt.ipDesc.String(); result != tt.result {
				t.Errorf("Expected %q, got %q", tt.result, result)
			}
		})
	}
}

func TestToIPDescError(t *testing.T) {
	tests := []struct {
		in  string
		out IPDesc
	}{
		{"", IPDesc{}},
		{":", IPDesc{}},
		{"abc:", IPDesc{}},
		{":abc", IPDesc{}},
		{"abc:abc", IPDesc{}},
		{"127.0.0.1:", IPDesc{}},
		{":1", IPDesc{}},
		{"::1", IPDesc{}},
		{"::1:42", IPDesc{}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			result, err := ToIPDesc(tt.in)
			if err == nil {
				t.Errorf("Unexpected success")
			}
			if !tt.out.Equal(result) {
				t.Errorf("Expected %v, got %v", tt.out, result)
			}
		})
	}
}

func TestToIPDesc(t *testing.T) {
	tests := []struct {
		in  string
		out IPDesc
	}{
		{"127.0.0.1:42", IPDesc{net.ParseIP("127.0.0.1"), 42}},
		{"[::1]:42", IPDesc{net.ParseIP("::1"), 42}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			result, err := ToIPDesc(tt.in)
			if err != nil {
				t.Errorf("Unexpected error %v", err)
			}
			if !tt.out.Equal(result) {
				t.Errorf("Expected %#v, got %#v", tt.out, result)
			}
		})
	}
}
