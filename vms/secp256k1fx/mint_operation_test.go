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

	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/vms/components/verify"
)

func TestMintOperationVerifyNil(t *testing.T) {
	op := (*MintOperation)(nil)
	if err := op.Verify(); err == nil {
		t.Fatalf("MintOperation.Verify should have returned an error due to an nil operation")
	}
}

func TestMintOperationOuts(t *testing.T) {
	op := &MintOperation{
		MintInput: Input{
			SigIndices: []uint32{0},
		},
		MintOutput: MintOutput{
			OutputOwners: OutputOwners{
				Threshold: 1,
				Addrs: []ids.ShortID{
					addr,
				},
			},
		},
		TransferOutput: TransferOutput{
			Amt: 1,
			OutputOwners: OutputOwners{
				Locktime:  0,
				Threshold: 1,
			},
		},
	}

	outs := op.Outs()
	if len(outs) != 2 {
		t.Fatalf("Wrong number of outputs")
	}
}

func TestMintOperationState(t *testing.T) {
	intf := interface{}(&MintOperation{})
	if _, ok := intf.(verify.State); ok {
		t.Fatalf("shouldn't be marked as state")
	}
}
