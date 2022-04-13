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

package propertyfx

import (
	"testing"

	"github.com/chain4travel/caminogo/vms/components/verify"
	"github.com/chain4travel/caminogo/vms/secp256k1fx"
)

func TestMintOperationVerifyNil(t *testing.T) {
	op := (*MintOperation)(nil)
	if err := op.Verify(); err == nil {
		t.Fatalf("nil operation should have failed verification")
	}
}

func TestMintOperationVerifyInvalidOutput(t *testing.T) {
	op := MintOperation{
		OwnedOutput: OwnedOutput{
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
			},
		},
	}
	if err := op.Verify(); err == nil {
		t.Fatalf("operation should have failed verification")
	}
}

func TestMintOperationOuts(t *testing.T) {
	op := MintOperation{}
	if outs := op.Outs(); len(outs) != 2 {
		t.Fatalf("Wrong number of outputs returned")
	}
}

func TestMintOperationState(t *testing.T) {
	intf := interface{}(&MintOperation{})
	if _, ok := intf.(verify.State); ok {
		t.Fatalf("shouldn't be marked as state")
	}
}
