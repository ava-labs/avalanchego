// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package constants

import (
	"testing"
)

func TestGetHRP(t *testing.T) {
	tests := []struct {
		id  uint32
		hrp string
	}{
		{
			id:  MainnetID,
			hrp: MainnetHRP,
		},
		{
			id:  TestnetID,
			hrp: FujiHRP,
		},
		{
			id:  FujiID,
			hrp: FujiHRP,
		},
		{
			id:  LocalID,
			hrp: LocalHRP,
		},
		{
			id:  4294967295,
			hrp: FallbackHRP,
		},
	}
	for _, test := range tests {
		t.Run(test.hrp, func(t *testing.T) {
			if hrp := GetHRP(test.id); hrp != test.hrp {
				t.Fatalf("GetHRP(%d) returned %q but expected %q",
					test.id, hrp, test.hrp)
			}
		})
	}
}

func TestNetworkName(t *testing.T) {
	tests := []struct {
		id   uint32
		name string
	}{
		{
			id:   MainnetID,
			name: MainnetName,
		},
		{
			id:   TestnetID,
			name: FujiName,
		},
		{
			id:   FujiID,
			name: FujiName,
		},
		{
			id:   LocalID,
			name: LocalName,
		},
		{
			id:   4294967295,
			name: "network-4294967295",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if name := NetworkName(test.id); name != test.name {
				t.Fatalf("NetworkName(%d) returned %q but expected %q",
					test.id, name, test.name)
			}
		})
	}
}

func TestNetworkID(t *testing.T) {
	tests := []struct {
		name      string
		id        uint32
		shouldErr bool
	}{
		{
			name: MainnetName,
			id:   MainnetID,
		},
		{
			name: "MaInNeT",
			id:   MainnetID,
		},
		{
			name: TestnetName,
			id:   TestnetID,
		},
		{
			name: FujiName,
			id:   FujiID,
		},
		{
			name: LocalName,
			id:   LocalID,
		},
		{
			name: "network-4294967295",
			id:   4294967295,
		},
		{
			name: "4294967295",
			id:   4294967295,
		},
		{
			name:      "networ-4294967295",
			shouldErr: true,
		},
		{
			name:      "network-4294967295123123",
			shouldErr: true,
		},
		{
			name:      "4294967295123123",
			shouldErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			id, err := NetworkID(test.name)
			if err == nil && test.shouldErr {
				t.Fatalf("NetworkID(%q) returned %d but should have errored", test.name, test.id)
			}
			if err != nil && !test.shouldErr {
				t.Fatalf("NetworkID(%q) unexpectedly errored with: %s", test.name, err)
			}
			if id != test.id {
				t.Fatalf("NetworkID(%q) returned %d but expected %d", test.name, id, test.id)
			}
		})
	}
}
