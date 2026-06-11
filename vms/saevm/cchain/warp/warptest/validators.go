// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package warptest provides BLS warp validator sets for tests that need to
// produce signed warp messages.
package warptest

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	// Imported for [snowtest.Context] comment resolution.
	_ "github.com/ava-labs/avalanchego/snow/snowtest"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// Validators is a BLS warp validator set held in canonical (public-key-sorted)
// order, retaining each validator's signer so that the signer bitset of a
// signature it produces lines up with the validator set used to verify it.
type Validators struct {
	validators []*validators.Warp
	signers    []bls.Signer // parallel to validators
}

// NewValidators creates n validators, each with weight 1, a fresh NodeID, and
// a freshly generated BLS key.
func NewValidators(tb testing.TB, n int) *Validators {
	tb.Helper()

	nodeIDs := make([]ids.NodeID, n)
	for i := range nodeIDs {
		nodeIDs[i] = ids.GenerateTestNodeID()
	}

	signers := make([]bls.Signer, len(nodeIDs))
	for i := range signers {
		sk, err := localsigner.New()
		require.NoError(tb, err)
		signers[i] = sk
	}
	slices.SortFunc(signers, func(a, b bls.Signer) int {
		return bytes.Compare(
			bls.PublicKeyToUncompressedBytes(a.PublicKey()),
			bls.PublicKeyToUncompressedBytes(b.PublicKey()),
		)
	})

	vdrs := make([]*validators.Warp, len(nodeIDs))
	for i, sk := range signers {
		pk := sk.PublicKey()
		vdrs[i] = &validators.Warp{
			PublicKey:      pk,
			PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk),
			Weight:         1,
			NodeIDs:        []ids.NodeID{nodeIDs[i]},
		}
	}
	return &Validators{
		validators: vdrs,
		signers:    signers,
	}
}

// Sign signs msg with every validator and returns the signed message with a
// [warp.BitSetSignature] covering the whole set.
func (v *Validators) Sign(tb testing.TB, msg *warp.UnsignedMessage) *warp.Message {
	tb.Helper()

	signatures := make([]*bls.Signature, len(v.signers))
	signerIndices := set.NewBits()
	for i, sk := range v.signers {
		sig, err := sk.Sign(msg.Bytes())
		require.NoErrorf(tb, err, "%T.Sign(...)", sk)
		signatures[i] = sig
		signerIndices.Add(i)
	}

	aggSig, err := bls.AggregateSignatures(signatures)
	require.NoError(tb, err, "bls.AggregateSignatures(...)")

	signed, err := warp.NewMessage(
		msg,
		&warp.BitSetSignature{
			Signers:   signerIndices.Bytes(),
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
			ctx.SubnetID: validators.WarpSet{
				Validators:  vdrs.validators,
				TotalWeight: uint64(len(vdrs.validators)),
			},
		}, nil
	}
}
