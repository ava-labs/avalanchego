// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nftfx

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestTransferOutputVerifyNil(t *testing.T) {
	require := require.New(t)

	to := (*TransferOutput)(nil)
	require.ErrorIs(to.Verify(), errNilTransferOutput)
}

func TestTransferOutputLargePayload(t *testing.T) {
	require := require.New(t)

	to := TransferOutput{
		Payload: make([]byte, MaxPayloadSize+1),
	}
	require.ErrorIs(to.Verify(), errPayloadTooLarge)
}

func TestTransferOutputInvalidSecp256k1Output(t *testing.T) {
	require := require.New(t)

	to := TransferOutput{
		OutputOwners: secp256k1fx.OutputOwners{
			Addrs: []ids.ShortID{
				ids.ShortEmpty,
				ids.ShortEmpty,
			},
		},
	}
	require.ErrorIs(to.Verify(), secp256k1fx.ErrOutputUnoptimized)
}

func TestTransferOutputState(t *testing.T) {
	require := require.New(t)

	intf := interface{}(&TransferOutput{})
	_, ok := intf.(verify.State)
	require.True(ok)
}
