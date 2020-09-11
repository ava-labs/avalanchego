// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

func TestMintOutputOwnersVerifyNil(t *testing.T) {
	out := (*OutputOwners)(nil)
	if err := out.Verify(); err == nil {
		t.Fatalf("OutputOwners.Verify should have returned an error due to an nil output")
	}
}

func TestMintOutputOwnersExactEquals(t *testing.T) {
	out0 := (*OutputOwners)(nil)
	out1 := (*OutputOwners)(nil)
	if !out0.Equals(out1) {
		t.Fatalf("Outputs should have equaled")
	}
}

func TestMintOutputOwnersNotEqual(t *testing.T) {
	out0 := &OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			ids.ShortEmpty,
		},
	}
	out1 := &OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			ids.NewShortID([20]byte{1}),
		},
	}
	if out0.Equals(out1) {
		t.Fatalf("Outputs should not have equaled")
	}
}

func TestMintOutputOwnersNotSorted(t *testing.T) {
	out := &OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			ids.NewShortID([20]byte{1}),
			ids.NewShortID([20]byte{0}),
		},
	}
	if err := out.Verify(); err == nil {
		t.Fatalf("Verification should have failed due to unsorted addresses")
	}
	out.Sort()
	if err := out.Verify(); err != nil {
		t.Fatal(err)
	}
}
