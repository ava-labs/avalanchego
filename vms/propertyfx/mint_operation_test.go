// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package propertyfx

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestMintOperationVerifyNil(t *testing.T) {
	require := require.New(t)

	op := (*MintOperation)(nil)
	require.ErrorIs(op.Verify(), errNilMintOperation)
}

func TestMintOperationVerifyInvalidOutput(t *testing.T) {
	require := require.New(t)

	op := MintOperation{
		OwnedOutput: OwnedOutput{
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
			},
		},
	}
	require.ErrorIs(op.Verify(), secp256k1fx.ErrOutputUnspendable)
}

func TestMintOperationOuts(t *testing.T) {
	require := require.New(t)

	op := MintOperation{}
	require.Len(op.Outs(), 2)
}

func TestMintOperationState(t *testing.T) {
	require := require.New(t)

	intf := interface{}(&MintOperation{})
	_, ok := intf.(verify.State)
	require.False(ok)
}
