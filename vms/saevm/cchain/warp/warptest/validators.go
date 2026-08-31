// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package warptest provides BLS warp validator sets for tests that need to
// produce signed warp messages.
package warptest

import (
	"context"
	"testing"

	"github.com/ava-labs/libevm/libevm/options"
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

// Option configures the validator set built by [NewValidators].
type Option = options.Option[config]

type config struct {
	n       int
	nodeIDs []ids.NodeID
	signers []bls.Signer
}

// WithMinimum sets a lower bound on the number of validators. The set grows
// beyond n when more NodeIDs or signers are supplied.
func WithMinimum(n int) Option {
	return options.Func[config](func(c *config) {
		c.n = n
	})
}

// WithNodeIDs assigns nodeIDs to the validators. Validators without an assigned
// NodeID get a freshly generated one.
func WithNodeIDs(nodeIDs ...ids.NodeID) Option {
	return options.Func[config](func(c *config) {
		c.nodeIDs = nodeIDs
	})
}

// WithSigners assigns BLS signers to the validators. Validators without an
// assigned signer get a freshly generated key.
func WithSigners(signers ...bls.Signer) Option {
	return options.Func[config](func(c *config) {
		c.signers = signers
	})
}

// NewValidators creates a BLS warp validator set, each validator with weight 1.
// The number of validators is the largest of the count set by [WithMinimum],
// the number of NodeIDs, and the number of signers. Any unspecified NodeIDs and
// signers are freshly generated.
func NewValidators(tb testing.TB, opts ...Option) *Validators {
	tb.Helper()

	c := options.As[config](opts...)
	n := max(c.n, len(c.nodeIDs), len(c.signers))
	for len(c.nodeIDs) < n {
		c.nodeIDs = append(c.nodeIDs, ids.GenerateTestNodeID())
	}
	for len(c.signers) < n {
		sk, err := localsigner.New()
		require.NoError(tb, err, "localsigner.New()")
		c.signers = append(c.signers, sk)
	}

	var (
		vdrs    = make([]*validators.Warp, n)
		signers = make(map[string]bls.Signer, n)
	)
	for i, nodeID := range c.nodeIDs {
		signer := c.signers[i]
		pk := signer.PublicKey()
		pkBytes := bls.PublicKeyToUncompressedBytes(pk)
		vdrs[i] = &validators.Warp{
			PublicKey:      pk,
			PublicKeyBytes: pkBytes,
			Weight:         1,
			NodeIDs:        []ids.NodeID{nodeID},
		}
		signers[string(pkBytes)] = signer
	}
	utils.Sort(vdrs)
	return &Validators{
		validators: vdrs,
		signers:    signers,
	}
}

// WarpSet returns the set as a [validators.WarpSet] whose TotalWeight is the
// sum of every validator's weight.
func (v *Validators) WarpSet() validators.WarpSet {
	var totalWeight uint64
	for _, vdr := range v.validators {
		totalWeight += vdr.Weight
	}
	return validators.WarpSet{
		Validators:  v.validators,
		TotalWeight: totalWeight,
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

// SetValidators makes ctx serve vdrs as the local validator set for both
// GetWarpValidatorSets and GetValidatorSet.
//
// ctx.ValidatorState MUST be a [validatorstest.State], which is the concrete
// type installed by [snowtest.Context].
func SetValidators(tb testing.TB, ctx *snow.Context, vdrs *Validators) {
	tb.Helper()

	vdrState, ok := ctx.ValidatorState.(*validatorstest.State)
	require.Truef(tb, ok, "unexpected type %T for validator state", ctx.ValidatorState)
	vdrState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		out := make(map[ids.NodeID]*validators.GetValidatorOutput, len(vdrs.validators))
		for _, vdr := range vdrs.validators {
			for _, nodeID := range vdr.NodeIDs {
				out[nodeID] = &validators.GetValidatorOutput{
					NodeID:    nodeID,
					PublicKey: vdr.PublicKey,
					Weight:    vdr.Weight,
				}
			}
		}
		return out, nil
	}
	vdrState.GetWarpValidatorSetsF = func(context.Context, uint64) (map[ids.ID]validators.WarpSet, error) {
		return map[ids.ID]validators.WarpSet{
			ctx.SubnetID: vdrs.WarpSet(),
		}, nil
	}
}
