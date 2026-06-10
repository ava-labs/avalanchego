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
	return NewValidatorsWithNodeIDs(tb, nodeIDs...)
}

// NewValidatorsWithNodeIDs creates one validator per nodeID, each with weight
// 1 and a freshly generated BLS key.
func NewValidatorsWithNodeIDs(tb testing.TB, nodeIDs ...ids.NodeID) *Validators {
	tb.Helper()

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

// Set returns the validator set in the form expected during verification.
func (v *Validators) Set() validators.WarpSet {
	return validators.WarpSet{
		Validators:  v.validators,
		TotalWeight: uint64(len(v.validators)),
	}
}

// NodeIDs returns the NodeIDs of every validator in the set.
func (v *Validators) NodeIDs() set.Set[ids.NodeID] {
	nodeIDs := set.NewSet[ids.NodeID](len(v.validators))
	for _, vdr := range v.validators {
		nodeIDs.Add(vdr.NodeIDs...)
	}
	return nodeIDs
}

// ValidatorSet returns the validators in the form returned by
// [validators.State.GetValidatorSet].
func (v *Validators) ValidatorSet() map[ids.NodeID]*validators.GetValidatorOutput {
	out := make(map[ids.NodeID]*validators.GetValidatorOutput, len(v.validators))
	for _, vdr := range v.validators {
		for _, nodeID := range vdr.NodeIDs {
			out[nodeID] = &validators.GetValidatorOutput{
				NodeID:    nodeID,
				PublicKey: vdr.PublicKey,
				Weight:    vdr.Weight,
			}
		}
	}
	return out
}

// Sign signs msg with every validator and returns the signed message with a
// [warp.BitSetSignature] covering the whole set.
func (v *Validators) Sign(tb testing.TB, msg *warp.UnsignedMessage) *warp.Message {
	tb.Helper()

	signatures := make([]*bls.Signature, len(v.signers))
	signerIndices := set.NewBits()
	for i, sk := range v.signers {
		sig, err := sk.Sign(msg.Bytes())
		require.NoError(tb, err)
		signatures[i] = sig
		signerIndices.Add(i)
	}

	aggregateSignature, err := bls.AggregateSignatures(signatures)
	require.NoError(tb, err)
	warpSignature := &warp.BitSetSignature{
		Signers: signerIndices.Bytes(),
	}
	copy(warpSignature.Signature[:], bls.SignatureToBytes(aggregateSignature))

	signedMsg, err := warp.NewMessage(msg, warpSignature)
	require.NoError(tb, err)
	return signedMsg
}

// State returns a validator state that attributes every chain to subnetID and
// serves v as that subnet's validator set, both regular and warp.
func (v *Validators) State(subnetID ids.ID) *validatorstest.State {
	return &validatorstest.State{
		GetSubnetIDF: func(context.Context, ids.ID) (ids.ID, error) {
			return subnetID, nil
		},
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			return v.ValidatorSet(), nil
		},
		GetWarpValidatorSetsF: func(context.Context, uint64) (map[ids.ID]validators.WarpSet, error) {
			return map[ids.ID]validators.WarpSet{
				subnetID: v.Set(),
			}, nil
		},
	}
}

// SetValidators makes state serve vdrs as subnetID's validator set, both
// regular (GetValidatorSet) and warp (GetWarpValidatorSets).
//
// state MUST be a [validatorstest.State], which is the concrete type installed
// by [snowtest.Context]. It is accepted as the [validators.State] interface
// rather than the concrete type so that callers can pass
// snowCtx.ValidatorState directly without each repeating the type assertion
// this helper exists to share.
func SetValidators(tb testing.TB, state validators.State, subnetID ids.ID, vdrs *Validators) {
	tb.Helper()

	vdrState, ok := state.(*validatorstest.State)
	require.Truef(tb, ok, "unexpected type %T for validator state", state)
	vdrState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		return vdrs.ValidatorSet(), nil
	}
	vdrState.GetWarpValidatorSetsF = func(context.Context, uint64) (map[ids.ID]validators.WarpSet, error) {
		return map[ids.ID]validators.WarpSet{
			subnetID: vdrs.Set(),
		}, nil
	}
}

// FakeSign returns msg with a syntactically valid but cryptographically
// invalid signature.
func FakeSign(tb testing.TB, msg *warp.UnsignedMessage) *warp.Message {
	tb.Helper()

	signed, err := warp.NewMessage(
		msg,
		&warp.BitSetSignature{
			Signers: set.NewBits().Bytes(),
		},
	)
	require.NoError(tb, err)
	return signed
}
