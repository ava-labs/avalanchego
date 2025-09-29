// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

func newWarp(t *testing.T) *Warp {
	t.Helper()

	sk, err := localsigner.New()
	require.NoError(t, err)

	nodeID := ids.GenerateTestNodeID()
	pk := sk.PublicKey()
	return &Warp{
		PublicKey:      pk,
		PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk),
		Weight:         3,
		NodeIDs:        []ids.NodeID{nodeID},
	}
}

func newWarpSet(t *testing.T, n uint64) WarpSet {
	t.Helper()

	vdrs := make([]*Warp, n)
	for i := range vdrs {
		vdrs[i] = newWarp(t)
	}
	utils.Sort(vdrs)
	return WarpSet{
		Validators:  vdrs,
		TotalWeight: 3 * n,
	}
}

func warpToOutput(w *Warp) *GetValidatorOutput {
	return &GetValidatorOutput{
		NodeID:    w.NodeIDs[0],
		PublicKey: w.PublicKey,
		Weight:    w.Weight,
	}
}

func TestFlattenValidatorSet(t *testing.T) {
	var (
		vdrs    = newWarpSet(t, 3)
		nodeID0 = vdrs.Validators[0].NodeIDs[0]
		nodeID1 = vdrs.Validators[1].NodeIDs[0]
		nodeID2 = vdrs.Validators[2].NodeIDs[0]
	)
	tests := []struct {
		name       string
		validators map[ids.NodeID]*GetValidatorOutput
		want       WarpSet
		wantErr    error
	}{
		{
			name: "overflow",
			validators: map[ids.NodeID]*GetValidatorOutput{
				nodeID0: warpToOutput(vdrs.Validators[0]),
				nodeID1: {
					NodeID:    nodeID1,
					PublicKey: vdrs.Validators[1].PublicKey,
					Weight:    math.MaxUint64,
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name: "nil_public_key_skipped",
			validators: map[ids.NodeID]*GetValidatorOutput{
				nodeID0: warpToOutput(vdrs.Validators[0]),
				nodeID1: {
					NodeID:    nodeID1,
					PublicKey: nil,
					Weight:    1,
				},
			},
			want: WarpSet{
				Validators:  []*Warp{vdrs.Validators[0]},
				TotalWeight: vdrs.Validators[0].Weight + 1,
			},
		},
		{
			name: "sorted", // Would non-deterministically fail without sorting
			validators: map[ids.NodeID]*GetValidatorOutput{
				nodeID0: warpToOutput(vdrs.Validators[0]),
				nodeID1: warpToOutput(vdrs.Validators[1]),
				nodeID2: warpToOutput(vdrs.Validators[2]),
			},
			want: vdrs,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			got, err := FlattenValidatorSet(test.validators)
			require.ErrorIs(err, test.wantErr)
			require.Equal(test.want, got)
		})
	}
}

func BenchmarkFlattenValidatorSet(b *testing.B) {
	for size := 1; size <= 1<<10; size *= 2 {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			vdrs := make(
				map[ids.NodeID]*GetValidatorOutput,
				size,
			)
			for range size {
				nodeID := ids.GenerateTestNodeID()
				secretKey, err := localsigner.New()
				require.NoError(b, err)
				publicKey := secretKey.PublicKey()
				vdrs[nodeID] = &GetValidatorOutput{
					NodeID:    nodeID,
					PublicKey: publicKey,
					Weight:    1,
				}
			}
			for b.Loop() {
				_, err := FlattenValidatorSet(vdrs)
				require.NoError(b, err)
			}
		})
	}
}
