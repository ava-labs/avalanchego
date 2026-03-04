// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package propertyfx

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestBurnOperationInvalid(t *testing.T) {
	op := BurnOperation{Input: secp256k1fx.Input{
		SigIndices: []uint32{1, 0},
	}}
	err := op.Verify()
	require.ErrorIs(t, err, secp256k1fx.ErrInputIndicesNotSortedUnique)
}

func TestBurnOperationNumberOfOutput(t *testing.T) {
	op := BurnOperation{}
	require.Empty(t, op.Outs())
}

func TestBurnOperationState(t *testing.T) {
	intf := interface{}(&BurnOperation{})
	_, ok := intf.(verify.State)
	require.False(t, ok)
}
