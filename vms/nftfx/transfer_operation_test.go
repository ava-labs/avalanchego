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

package nftfx

import (
	"testing"

	"github.com/chain4travel/caminogo/vms/components/verify"
	"github.com/chain4travel/caminogo/vms/secp256k1fx"
)

func TestTransferOperationVerifyNil(t *testing.T) {
	op := (*TransferOperation)(nil)
	if err := op.Verify(); err == nil {
		t.Fatalf("nil operation should have failed verification")
	}
}

func TestTransferOperationInvalid(t *testing.T) {
	op := TransferOperation{Input: secp256k1fx.Input{
		SigIndices: []uint32{1, 0},
	}}
	if err := op.Verify(); err == nil {
		t.Fatalf("operation should have failed verification")
	}
}

func TestTransferOperationOuts(t *testing.T) {
	op := TransferOperation{
		Output: TransferOutput{},
	}
	if outs := op.Outs(); len(outs) != 1 {
		t.Fatalf("Wrong number of outputs returned")
	}
}

func TestTransferOperationState(t *testing.T) {
	intf := interface{}(&TransferOperation{})
	if _, ok := intf.(verify.State); ok {
		t.Fatalf("shouldn't be marked as state")
	}
}
