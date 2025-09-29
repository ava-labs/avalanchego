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

type testWarp struct {
	nodeID ids.NodeID
	sk     bls.Signer
	vdr    *Warp
}

func (v *testWarp) Compare(o *testWarp) int {
	return v.vdr.Compare(o.vdr)
}

func newTestWarp(t *testing.T) *testWarp {
	t.Helper()

	sk, err := localsigner.New()
	require.NoError(t, err)

	nodeID := ids.GenerateTestNodeID()
	pk := sk.PublicKey()
	return &testWarp{
		nodeID: nodeID,
		sk:     sk,
		vdr: &Warp{
			PublicKey:      pk,
			PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk),
			Weight:         3,
			NodeIDs:        []ids.NodeID{nodeID},
		},
	}
}

func newTestWarpSet(t *testing.T, n int) []*testWarp {
	vdrs := make([]*testWarp, n)
	for i := range vdrs {
		vdrs[i] = newTestWarp(t)
	}
	utils.Sort(vdrs)
	return vdrs
}

func TestFlattenValidatorSet(t *testing.T) {
	vdrs := newTestWarpSet(t, 3)
	tests := []struct {
		name       string
		validators map[ids.NodeID]*GetValidatorOutput
		want       WarpSet
		wantErr    error
	}{
		{
			name: "overflow",
			validators: map[ids.NodeID]*GetValidatorOutput{
				vdrs[0].nodeID: {
					NodeID:    vdrs[0].nodeID,
					PublicKey: vdrs[0].vdr.PublicKey,
					Weight:    math.MaxUint64,
				},
				vdrs[1].nodeID: {
					NodeID:    vdrs[1].nodeID,
					PublicKey: vdrs[1].vdr.PublicKey,
					Weight:    1,
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name: "nil_public_key_skipped",
			validators: map[ids.NodeID]*GetValidatorOutput{
				vdrs[0].nodeID: {
					NodeID:    vdrs[0].nodeID,
					PublicKey: vdrs[0].vdr.PublicKey,
					Weight:    vdrs[0].vdr.Weight,
				},
				vdrs[1].nodeID: {
					NodeID:    vdrs[1].nodeID,
					PublicKey: nil,
					Weight:    1,
				},
			},
			want: WarpSet{
				Validators:  []*Warp{vdrs[0].vdr},
				TotalWeight: vdrs[0].vdr.Weight + 1,
			},
		},
		{
			name: "sorted", // Would non-deterministically fail without sorting
			validators: map[ids.NodeID]*GetValidatorOutput{
				vdrs[0].nodeID: {
					NodeID:    vdrs[0].nodeID,
					PublicKey: vdrs[0].vdr.PublicKey,
					Weight:    vdrs[0].vdr.Weight,
				},
				vdrs[1].nodeID: {
					NodeID:    vdrs[1].nodeID,
					PublicKey: vdrs[1].vdr.PublicKey,
					Weight:    vdrs[1].vdr.Weight,
				},
				vdrs[2].nodeID: {
					NodeID:    vdrs[2].nodeID,
					PublicKey: vdrs[2].vdr.PublicKey,
					Weight:    vdrs[2].vdr.Weight,
				},
			},
			want: WarpSet{
				Validators:  []*Warp{vdrs[0].vdr, vdrs[1].vdr, vdrs[2].vdr},
				TotalWeight: vdrs[0].vdr.Weight + vdrs[1].vdr.Weight + vdrs[2].vdr.Weight,
			},
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
