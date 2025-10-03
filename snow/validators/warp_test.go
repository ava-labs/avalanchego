// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators_test

import (
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"

	safemath "github.com/ava-labs/avalanchego/utils/math"

	. "github.com/ava-labs/avalanchego/snow/validators"
)

func TestFlattenValidatorSet(t *testing.T) {
	var (
		vdrs    = validatorstest.NewWarpSet(t, 3)
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
				nodeID0: validatorstest.WarpToOutput(vdrs.Validators[0]),
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
				nodeID0: validatorstest.WarpToOutput(vdrs.Validators[0]),
				nodeID1: {
					NodeID:    nodeID1,
					PublicKey: nil,
					Weight:    5, // don't use 1 to distinguish from default
				},
			},
			want: WarpSet{
				Validators:  []*Warp{vdrs.Validators[0]},
				TotalWeight: vdrs.Validators[0].Weight + 5,
			},
		},
		{
			name: "sorted", // Would non-deterministically fail without sorting
			validators: map[ids.NodeID]*GetValidatorOutput{
				nodeID0: validatorstest.WarpToOutput(vdrs.Validators[0]),
				nodeID1: validatorstest.WarpToOutput(vdrs.Validators[1]),
				nodeID2: validatorstest.WarpToOutput(vdrs.Validators[2]),
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
