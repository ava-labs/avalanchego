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
		}, {
			IPDesc{net.ParseIP("::1"), 0},
			IPDesc{net.ParseIP("::1"), 0},
			true,
		}, {
			IPDesc{net.ParseIP("127.0.0.1"), 0},
			IPDesc{net.ParseIP("::ffff:127.0.0.1"), 0},
			true,
		},

		// Expected unequal
		{
			IPDesc{net.ParseIP("127.0.0.1"), 0},
			IPDesc{net.ParseIP("1.2.3.4"), 0},
			false,
		}, {
			IPDesc{net.ParseIP("::1"), 0},
			IPDesc{net.ParseIP("2001::1"), 0},
			false,
		}, {
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
