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

var (
	_ utils.Sortable[*testWarp] = (*testWarp)(nil)

	testVdrs []*testWarp
)

type testWarp struct {
	nodeID ids.NodeID
	sk     bls.Signer
	vdr    *Warp
}

func (v *testWarp) Compare(o *testWarp) int {
	return v.vdr.Compare(o.vdr)
}

func newTestValidator() *testWarp {
	sk, err := localsigner.New()
	if err != nil {
		panic(err)
	}

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

func init() {
	testVdrs = []*testWarp{
		newTestValidator(),
		newTestValidator(),
		newTestValidator(),
	}
	utils.Sort(testVdrs)
}

func TestFlattenValidatorSet(t *testing.T) {
	tests := []struct {
		name       string
		validators map[ids.NodeID]*GetValidatorOutput
		want       WarpSet
		wantErr    error
	}{
		{
			name: "overflow",
			validators: map[ids.NodeID]*GetValidatorOutput{
				testVdrs[0].nodeID: {
					NodeID:    testVdrs[0].nodeID,
					PublicKey: testVdrs[0].vdr.PublicKey,
					Weight:    math.MaxUint64,
				},
				testVdrs[1].nodeID: {
					NodeID:    testVdrs[1].nodeID,
					PublicKey: testVdrs[1].vdr.PublicKey,
					Weight:    1,
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name: "nil_public_key_skipped",
			validators: map[ids.NodeID]*GetValidatorOutput{
				testVdrs[0].nodeID: {
					NodeID:    testVdrs[0].nodeID,
					PublicKey: testVdrs[0].vdr.PublicKey,
					Weight:    testVdrs[0].vdr.Weight,
				},
				testVdrs[1].nodeID: {
					NodeID:    testVdrs[1].nodeID,
					PublicKey: nil,
					Weight:    1,
				},
			},
			want: WarpSet{
				Validators:  []*Warp{testVdrs[0].vdr},
				TotalWeight: testVdrs[0].vdr.Weight + 1,
			},
		},
		{
			name: "sorted", // Would non-deterministically fail without sorting
			validators: map[ids.NodeID]*GetValidatorOutput{
				testVdrs[0].nodeID: {
					NodeID:    testVdrs[0].nodeID,
					PublicKey: testVdrs[0].vdr.PublicKey,
					Weight:    testVdrs[0].vdr.Weight,
				},
				testVdrs[1].nodeID: {
					NodeID:    testVdrs[1].nodeID,
					PublicKey: testVdrs[1].vdr.PublicKey,
					Weight:    testVdrs[1].vdr.Weight,
				},
				testVdrs[2].nodeID: {
					NodeID:    testVdrs[2].nodeID,
					PublicKey: testVdrs[2].vdr.PublicKey,
					Weight:    testVdrs[2].vdr.Weight,
				},
			},
			want: WarpSet{
				Validators:  []*Warp{testVdrs[0].vdr, testVdrs[1].vdr, testVdrs[2].vdr},
				TotalWeight: testVdrs[0].vdr.Weight + testVdrs[1].vdr.Weight + testVdrs[2].vdr.Weight,
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
