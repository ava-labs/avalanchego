// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package propertyfx

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestBurnOperationInvalid(t *testing.T) {
	require := require.New(t)

	op := BurnOperation{Input: secp256k1fx.Input{
		SigIndices: []uint32{1, 0},
	}}
	require.ErrorIs(op.Verify(), nil)
}

func TestBurnOperationNumberOfOutput(t *testing.T) {
	require := require.New(t)

	op := BurnOperation{}
	require.Empty(op.Outs())
}

func TestBurnOperationState(t *testing.T) {
	require := require.New(t)

	intf := interface{}(&BurnOperation{})
	_, ok := intf.(verify.State)
	require.False(ok)
}
