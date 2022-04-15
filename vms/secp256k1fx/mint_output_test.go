// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"

	"github.com/chain4travel/caminogo/vms/components/verify"
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
