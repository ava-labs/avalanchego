// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nftfx

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestTransferOperationVerifyNil(t *testing.T) {
	require := require.New(t)

	op := (*TransferOperation)(nil)
	require.ErrorIs(op.Verify(), errNilTransferOperation)
}

func TestTransferOperationInvalid(t *testing.T) {
	require := require.New(t)

	op := TransferOperation{Input: secp256k1fx.Input{
		SigIndices: []uint32{1, 0},
	}}
	require.ErrorIs(op.Verify(), secp256k1fx.ErrInputIndicesNotSortedUnique)
}

func TestTransferOperationOuts(t *testing.T) {
	require := require.New(t)

	op := TransferOperation{
		Output: TransferOutput{},
	}
	require.Len(op.Outs(), 1)
}

func TestTransferOperationState(t *testing.T) {
	require := require.New(t)

	intf := interface{}(&TransferOperation{})
	_, ok := intf.(verify.State)
	require.False(ok)
}
