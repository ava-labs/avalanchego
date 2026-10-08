// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp/warptest"

	corethwarp "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	evmprecompileconfig "github.com/ava-labs/avalanchego/graft/evm/precompileconfig"
)

// graniteRules satisfies [evmprecompileconfig.Rules] as post-Granite.
type graniteRules struct{}

func (graniteRules) IsGraniteActivated() bool { return true }
func (graniteRules) IsDurangoActivated() bool { return true }

func TestGasMatchesCorethGranite(t *testing.T) {
	want := corethwarp.CurrentGasConfig(graniteRules{})
	require.Equal(t, want.GetBlockchainID, Gas.GetBlockchainID, "GetBlockchainID")
	require.Equal(t, want.GetVerifiedWarpMessageBase, Gas.GetVerifiedWarpMessageBase, "GetVerifiedWarpMessageBase")
	require.Equal(t, want.PerWarpSigner, Gas.PerWarpSigner, "PerWarpSigner")
	require.Equal(t, want.PerWarpMessageChunk, Gas.PerWarpMessageChunk, "PerWarpMessageChunk")
	require.Equal(t, want.VerifyPredicateBase, Gas.VerifyPredicateBase, "VerifyPredicateBase")
	require.Equal(t, want.SendWarpMessageBase, Gas.SendWarpMessageBase, "SendWarpMessageBase")
	require.Equal(t, want.PerWarpMessageByte, Gas.PerWarpMessageByte, "PerWarpMessageByte")
}

func TestPredicateGas(t *testing.T) {
	vdrs := warptest.NewValidators(t, warptest.WithMinimum(3))
	msg, _ := newAddressedCall(t)
	signed := vdrs.Sign(t, msg)
	pred := predicate.New(signed.Bytes())

	numSigners, err := signed.Signature.NumSigners()
	require.NoError(t, err, "NumSigners()")

	// This is the formula coreth charges; `pred` is measured in 32-byte chunks.
	want := Gas.VerifyPredicateBase +
		Gas.PerWarpMessageChunk*uint64(len(pred)) +
		Gas.PerWarpSigner*uint64(numSigners) //#nosec G115 -- NumSigners is non-negative

	got, err := Predicater{}.PredicateGas(pred, graniteRules{})
	require.NoError(t, err, "PredicateGas()")
	require.Equal(t, want, got, "PredicateGas()")

	// Coreth's predicater under Granite must agree exactly.
	coreth, err := corethwarp.NewDefaultConfig(new(uint64)).PredicateGas(pred, graniteRules{})
	require.NoError(t, err, "corethwarp PredicateGas()")
	require.Equal(t, coreth, got, "PredicateGas() vs coreth")
}

func TestPredicateGasErrors(t *testing.T) {
	tests := []struct {
		name    string
		pred    predicate.Predicate
		wantErr error
	}{
		{
			name:    "invalid_packing",
			pred:    predicate.Predicate{{}}, // no 0xff delimiter
			wantErr: errInvalidPredicateBytes,
		},
		{
			name:    "not_a_warp_message",
			pred:    predicate.New([]byte("not a message")),
			wantErr: errInvalidWarpMsg,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Predicater{}.PredicateGas(tt.pred, graniteRules{})
			require.ErrorIs(t, err, tt.wantErr, "PredicateGas()")
		})
	}
}

func TestVerifyPredicate(t *testing.T) {
	vdrs := warptest.NewValidators(t, warptest.WithMinimum(2))
	ctx := snowtest.Context(t, snowtest.CChainID)
	warptest.SetValidators(t, ctx, vdrs)
	pc := &evmprecompileconfig.PredicateContext{
		SnowCtx:            ctx,
		ProposerVMBlockCtx: &block.Context{},
	}

	msg, _ := newAddressedCall(t)

	tests := []struct {
		name    string
		pred    predicate.Predicate
		wantErr error
	}{
		{
			name: "valid",
			pred: predicate.New(vdrs.Sign(t, msg).Bytes()),
		},
		{
			name:    "bad_signature",
			pred:    predicate.New(warptest.IncorrectlySign(t, msg).Bytes()),
			wantErr: errFailedVerification,
		},
		{
			name:    "invalid_packing",
			pred:    predicate.Predicate{{}},
			wantErr: errInvalidPredicateBytes,
		},
		{
			name:    "not_a_warp_message",
			pred:    predicate.New([]byte("not a message")),
			wantErr: errCannotParseWarpMsg,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Predicater{}.VerifyPredicate(pc, tt.pred)
			require.ErrorIs(t, err, tt.wantErr, "VerifyPredicate()")
		})
	}
}

// TestVerifyPredicateSourcePrimaryNetwork pins that a message from a
// primary-network chain is verified against this chain's own validator set,
// which is coreth's behaviour with RequirePrimaryNetworkSigners=false.
func TestVerifyPredicateSourcePrimaryNetwork(t *testing.T) {
	vdrs := warptest.NewValidators(t, warptest.WithMinimum(2))
	ctx := snowtest.Context(t, snowtest.CChainID)
	// snowtest places this chain on the primary network, which would make the
	// substitution a no-op. Moving it to another subnet means the only
	// validator set available is keyed by that subnet, not by the primary
	// network, so verification succeeds only if the substitution happens.
	ctx.SubnetID = ids.GenerateTestID()
	warptest.SetValidators(t, ctx, vdrs) // keys the validator sets by ctx.SubnetID
	pc := &evmprecompileconfig.PredicateContext{SnowCtx: ctx, ProposerVMBlockCtx: &block.Context{}}

	// newAddressedCall sources the message from the X-chain, which is on the
	// primary network, so the source subnet must be replaced by ctx.SubnetID
	// for its validator set to be found.
	msg, _ := newAddressedCall(t)
	require.Equal(t, snowtest.XChainID, msg.SourceChainID, "test message source")
	require.Equal(t, constants.UnitTestID, msg.NetworkID, "test message network")

	err := Predicater{}.VerifyPredicate(pc, predicate.New(vdrs.Sign(t, msg).Bytes()))
	require.NoError(t, err, "VerifyPredicate(primary-network source)")
}
