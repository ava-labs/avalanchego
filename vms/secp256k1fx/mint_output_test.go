// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/components/verify"
)

func TestMintOutputVerifyNil(t *testing.T) {
	require := require.New(t)
	out := (*MintOutput)(nil)
	require.ErrorIs(out.Verify(), errNilOutput)
}

func TestMintOutputState(t *testing.T) {
	require := require.New(t)
	intf := interface{}(&MintOutput{})
	_, ok := intf.(verify.State)
	require.True(ok)
}
