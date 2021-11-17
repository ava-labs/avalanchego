// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package propertyfx

import (
	"testing"

	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestBurnOperationInvalid(t *testing.T) {
	op := BurnOperation{Input: secp256k1fx.Input{
		SigIndices: []uint32{1, 0},
	}}
	if err := op.Verify(); err == nil {
		t.Fatalf("operation should have failed verification")
	}
}

func TestBurnOperationNumberOfOutput(t *testing.T) {
	op := BurnOperation{}
	if outs := op.Outs(); len(outs) != 0 {
		t.Fatalf("wrong number of outputs")
	}
}

func TestBurnOperationState(t *testing.T) {
	intf := interface{}(&BurnOperation{})
	if _, ok := intf.(verify.State); ok {
		t.Fatalf("shouldn't be marked as state")
	}
}
