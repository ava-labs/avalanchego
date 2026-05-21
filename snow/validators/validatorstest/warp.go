// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validatorstest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/MetalBlockchain/metalgo/ids"
	"github.com/MetalBlockchain/metalgo/snow/validators"
	"github.com/MetalBlockchain/metalgo/utils"
	"github.com/MetalBlockchain/metalgo/utils/crypto/bls"
	"github.com/MetalBlockchain/metalgo/utils/crypto/bls/signer/localsigner"
)

func NewWarp(t testing.TB, weight uint64) *validators.Warp {
	t.Helper()

	sk, err := localsigner.New()
	require.NoError(t, err)

	nodeID := ids.GenerateTestNodeID()
	pk := sk.PublicKey()
	return &validators.Warp{
		PublicKey:      pk,
		PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk),
		Weight:         weight,
		NodeIDs:        []ids.NodeID{nodeID},
	}
}

func NewWarpSet(t testing.TB, n uint64) validators.WarpSet {
	t.Helper()

	vdrs := make([]*validators.Warp, n)
	for i := range vdrs {
		vdrs[i] = NewWarp(t, 1)
	}
	utils.Sort(vdrs)
	return validators.WarpSet{
		Validators:  vdrs,
		TotalWeight: n,
	}
}

func WarpToOutput(w *validators.Warp) *validators.GetValidatorOutput {
	return &validators.GetValidatorOutput{
		NodeID:    w.NodeIDs[0],
		PublicKey: w.PublicKey,
		Weight:    w.Weight,
	}
}
