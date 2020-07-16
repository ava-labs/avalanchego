package secp256k1fx

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
)

func TestControlGroup(t *testing.T) {
	type test struct {
		description      string
		cg               ControlGroup
		shouldFailVerify bool
	}

	addr1 := ids.GenerateTestShortID()
	addr2 := ids.GenerateTestShortID()
	tests := []test{
		{
			"0 threshold, no addresses",
			ControlGroup{
				Threshold: 0,
				Addresses: []ids.ShortID{},
			},
			false,
		},
		{
			"threshold > len(addresses)",
			ControlGroup{
				Threshold: 2,
				Addresses: []ids.ShortID{addr1},
			},
			true,
		},
		{
			"threshold == len(addresses)",
			ControlGroup{
				Threshold: 2,
				Addresses: []ids.ShortID{addr1, addr2},
			},
			false,
		},
		{
			"threshold < len(addresses)",
			ControlGroup{
				Threshold: 1,
				Addresses: []ids.ShortID{addr1, addr2},
			},
			false,
		},
		{
			"repeat addresses",
			ControlGroup{
				Threshold: 1,
				Addresses: []ids.ShortID{addr1, addr1},
			},
			true,
		},
		{
			"too many addresses",
			ControlGroup{
				Threshold: 1,
				Addresses: make([]ids.ShortID, maxAddressesSize+1),
			},
			true,
		},
	}

	for _, test := range tests {
		if err := test.cg.Verify(); err != nil && !test.shouldFailVerify {
			t.Fatalf("test '%s' unexpectedly errored: %s", test.description, err)
		} else if err == nil && test.shouldFailVerify {
			t.Fatalf("test '%s' should have errored, but didn't", test.description)
		}
	}
}
