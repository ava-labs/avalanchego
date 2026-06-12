// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package warptest provides BLS warp validator sets for tests that need to
// produce signed warp messages.
package warptest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	// Imported for [snowtest.Context] comment resolution.
	_ "github.com/ava-labs/avalanchego/snow/snowtest"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// Validators is a BLS warp validator set held in canonical (public-key-sorted)
// order.
type Validators struct {
	validators []*validators.Warp
	signers    map[string]bls.Signer // pk bytes -> signer
}

// NewValidators creates n validators, each with weight 1, a fresh NodeID, and
// a freshly generated BLS key.
func NewValidators(tb testing.TB, n int) *Validators {
	tb.Helper()

	nodeIDs := make([]ids.NodeID, n)
	for i := range nodeIDs {
		nodeIDs[i] = ids.GenerateTestNodeID()
	}

	var (
		vdrs    = make([]*validators.Warp, len(nodeIDs))
		signers = make(map[string]bls.Signer, len(nodeIDs))
	)
	for i, nodeID := range nodeIDs {
		sk, err := localsigner.New()
		require.NoError(tb, err, "localsigner.New()")

		pk := sk.PublicKey()
		pkBytes := bls.PublicKeyToUncompressedBytes(pk)
		vdrs[i] = &validators.Warp{
			PublicKey:      pk,
			PublicKeyBytes: pkBytes,
			Weight:         1,
			NodeIDs:        []ids.NodeID{nodeID},
		}
		signers[string(pkBytes)] = sk
	}
	utils.Sort(vdrs)
	return &Validators{
		validators: vdrs,
		signers:    signers,
	}
}

// Sign signs msg with every validator and returns the signed message with a
// [warp.BitSetSignature] covering the whole set.
func (v *Validators) Sign(tb testing.TB, msg *warp.UnsignedMessage) *warp.Message {
	tb.Helper()

	var (
		sigs    = make([]*bls.Signature, len(v.validators))
		signers = set.NewBits()
	)
	for i, vdr := range v.validators {
		sk := v.signers[string(vdr.PublicKeyBytes)]
		sig, err := sk.Sign(msg.Bytes())
		require.NoErrorf(tb, err, "%T.Sign(...)", sk)
		sigs[i] = sig
		signers.Add(i)
	}

	aggSig, err := bls.AggregateSignatures(sigs)
	require.NoError(tb, err, "bls.AggregateSignatures(...)")

	signed, err := warp.NewMessage(
		msg,
		&warp.BitSetSignature{
			Signers:   signers.Bytes(),
			Signature: [bls.SignatureLen]byte(bls.SignatureToBytes(aggSig)),
		},
	)
	require.NoError(tb, err, "warp.NewMessage(...)")
	return signed
}

// IncorrectlySign returns msg with a syntactically valid but cryptographically
// invalid signature.
func IncorrectlySign(tb testing.TB, msg *warp.UnsignedMessage) *warp.Message {
	tb.Helper()

	signed, err := warp.NewMessage(
		msg,
		&warp.BitSetSignature{
			Signers: set.NewBits().Bytes(),
		},
	)
	require.NoError(tb, err, "warp.NewMessage(...)")
	return signed
}

// SetValidators makes ctx serve vdrs as the local validator set for
// GetWarpValidatorSets.
//
// ctx.ValidatorState MUST be a [validatorstest.State], which is the concrete
// type installed by [snowtest.Context].
func SetValidators(tb testing.TB, ctx *snow.Context, vdrs *Validators) {
	tb.Helper()

	vdrState, ok := ctx.ValidatorState.(*validatorstest.State)
	require.Truef(tb, ok, "unexpected type %T for validator state", ctx.ValidatorState)
	vdrState.GetWarpValidatorSetsF = func(context.Context, uint64) (map[ids.ID]validators.WarpSet, error) {
		return map[ids.ID]validators.WarpSet{
			ctx.SubnetID: {
				Validators:  vdrs.validators,
				TotalWeight: uint64(len(vdrs.validators)),
			},
		}, nil
	}
}
