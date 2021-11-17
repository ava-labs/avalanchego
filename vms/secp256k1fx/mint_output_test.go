// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"

	"github.com/ava-labs/avalanchego/vms/components/verify"
)

func TestMintOutputVerifyNil(t *testing.T) {
	out := (*MintOutput)(nil)
	if err := out.Verify(); err == nil {
		t.Fatalf("MintOutput.Verify should have returned an error due to an nil output")
	}
}

func TestMintOutputState(t *testing.T) {
	intf := interface{}(&MintOutput{})
	if _, ok := intf.(verify.State); !ok {
		t.Fatalf("should be marked as state")
	}
}
