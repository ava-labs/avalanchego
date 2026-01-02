// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorsmock"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/set"
)

func TestGetCanonicalValidatorSet(t *testing.T) {
	type test struct {
		name           string
		stateF         func(*gomock.Controller) validators.State
		expectedVdrs   []*validators.Warp
		expectedWeight uint64
		expectedErr    error
	}

	tests := []test{
		{
			name: "can't get validator set",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(nil, errTest)
				return state
			},
			expectedErr: errTest,
		},
		{
			name: "all validators have public keys; no duplicate pub keys",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(
					map[ids.NodeID]*validators.GetValidatorOutput{
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
					},
					nil,
				)
				return state
			},
			expectedVdrs:   []*validators.Warp{testVdrs[0].vdr, testVdrs[1].vdr},
			expectedWeight: 6,
			expectedErr:    nil,
		},
		{
			name: "all validators have public keys; duplicate pub keys",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(
					map[ids.NodeID]*validators.GetValidatorOutput{
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
							PublicKey: testVdrs[0].vdr.PublicKey,
							Weight:    testVdrs[0].vdr.Weight,
						},
					},
					nil,
				)
				return state
			},
			expectedVdrs: []*validators.Warp{
				{
					PublicKey:      testVdrs[0].vdr.PublicKey,
					PublicKeyBytes: testVdrs[0].vdr.PublicKeyBytes,
					Weight:         testVdrs[0].vdr.Weight * 2,
					NodeIDs: []ids.NodeID{
						testVdrs[0].nodeID,
						testVdrs[2].nodeID,
					},
				},
				testVdrs[1].vdr,
			},
			expectedWeight: 9,
			expectedErr:    nil,
		},
		{
			name: "validator without public key; no duplicate pub keys",
			stateF: func(ctrl *gomock.Controller) validators.State {
				state := validatorsmock.NewState(ctrl)
				state.EXPECT().GetValidatorSet(gomock.Any(), pChainHeight, subnetID).Return(
					map[ids.NodeID]*validators.GetValidatorOutput{
						testVdrs[0].nodeID: {
							NodeID:    testVdrs[0].nodeID,
							PublicKey: nil,
							Weight:    testVdrs[0].vdr.Weight,
						},
						testVdrs[1].nodeID: {
							NodeID:    testVdrs[1].nodeID,
							PublicKey: testVdrs[1].vdr.PublicKey,
							Weight:    testVdrs[1].vdr.Weight,
						},
					},
					nil,
				)
				return state
			},
			expectedVdrs:   []*validators.Warp{testVdrs[1].vdr},
			expectedWeight: 6,
			expectedErr:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			state := tt.stateF(ctrl)

			validators, err := GetCanonicalValidatorSetFromSubnetID(t.Context(), state, pChainHeight, subnetID)
			require.ErrorIs(err, tt.expectedErr)
			if err != nil {
				return
			}
			require.Equal(tt.expectedWeight, validators.TotalWeight)

			// These are pointers so have to test equality like this
			require.Len(validators.Validators, len(tt.expectedVdrs))
			for i, expectedVdr := range tt.expectedVdrs {
				gotVdr := validators.Validators[i]
				expectedPKBytes := bls.PublicKeyToCompressedBytes(expectedVdr.PublicKey)
				gotPKBytes := bls.PublicKeyToCompressedBytes(gotVdr.PublicKey)
				require.Equal(expectedPKBytes, gotPKBytes)
				require.Equal(expectedVdr.PublicKeyBytes, gotVdr.PublicKeyBytes)
				require.Equal(expectedVdr.Weight, gotVdr.Weight)
				require.ElementsMatch(expectedVdr.NodeIDs, gotVdr.NodeIDs)
			}
		})
	}
}

func TestFilterValidators(t *testing.T) {
	sk0, err := localsigner.New()
	require.NoError(t, err)
	pk0 := sk0.PublicKey()
	vdr0 := &validators.Warp{
		PublicKey:      pk0,
		PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk0),
		Weight:         1,
	}

	sk1, err := localsigner.New()
	require.NoError(t, err)
	pk1 := sk1.PublicKey()
	vdr1 := &validators.Warp{
		PublicKey:      pk1,
		PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk1),
		Weight:         2,
	}

	type test struct {
		name         string
		indices      set.Bits
		vdrs         []*validators.Warp
		expectedVdrs []*validators.Warp
		expectedErr  error
	}

	tests := []test{
		{
			name:         "empty",
			indices:      set.NewBits(),
			vdrs:         []*validators.Warp{},
			expectedVdrs: []*validators.Warp{},
			expectedErr:  nil,
		},
		{
			name:        "unknown validator",
			indices:     set.NewBits(2),
			vdrs:        []*validators.Warp{vdr0, vdr1},
			expectedErr: ErrUnknownValidator,
		},
		{
			name:    "two filtered out",
			indices: set.NewBits(),
			vdrs: []*validators.Warp{
				vdr0,
				vdr1,
			},
			expectedVdrs: []*validators.Warp{},
			expectedErr:  nil,
		},
		{
			name:    "one filtered out",
			indices: set.NewBits(1),
			vdrs: []*validators.Warp{
				vdr0,
				vdr1,
			},
			expectedVdrs: []*validators.Warp{
				vdr1,
			},
			expectedErr: nil,
		},
		{
			name:    "none filtered out",
			indices: set.NewBits(0, 1),
			vdrs: []*validators.Warp{
				vdr0,
				vdr1,
			},
			expectedVdrs: []*validators.Warp{
				vdr0,
				vdr1,
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			vdrs, err := FilterValidators(tt.indices, tt.vdrs)
			require.ErrorIs(err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}
			require.Equal(tt.expectedVdrs, vdrs)
		})
	}
}

func TestSumWeight(t *testing.T) {
	vdr0 := &validators.Warp{
		Weight: 1,
	}
	vdr1 := &validators.Warp{
		Weight: 2,
	}
	vdr2 := &validators.Warp{
		Weight: math.MaxUint64,
	}

	type test struct {
		name        string
		vdrs        []*validators.Warp
		expectedSum uint64
		expectedErr error
	}

	tests := []test{
		{
			name:        "empty",
			vdrs:        []*validators.Warp{},
			expectedSum: 0,
		},
		{
			name:        "one",
			vdrs:        []*validators.Warp{vdr0},
			expectedSum: 1,
		},
		{
			name:        "two",
			vdrs:        []*validators.Warp{vdr0, vdr1},
			expectedSum: 3,
		},
		{
			name:        "overflow",
			vdrs:        []*validators.Warp{vdr0, vdr2},
			expectedErr: ErrWeightOverflow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			sum, err := SumWeight(tt.vdrs)
			require.ErrorIs(err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}
			require.Equal(tt.expectedSum, sum)
		})
	}
}

func BenchmarkGetCanonicalValidatorSet(b *testing.B) {
	pChainHeight := uint64(1)
	subnetID := ids.GenerateTestID()
	numNodes := 10_000
	getValidatorOutputs := make([]*validators.GetValidatorOutput, 0, numNodes)
	for i := 0; i < numNodes; i++ {
		nodeID := ids.GenerateTestNodeID()
		blsPrivateKey, err := localsigner.New()
		require.NoError(b, err)
		blsPublicKey := blsPrivateKey.PublicKey()
		getValidatorOutputs = append(getValidatorOutputs, &validators.GetValidatorOutput{
			NodeID:    nodeID,
			PublicKey: blsPublicKey,
			Weight:    20,
		})
	}

	for _, size := range []int{0, 1, 10, 100, 1_000, 10_000} {
		getValidatorsOutput := make(map[ids.NodeID]*validators.GetValidatorOutput)
		for i := 0; i < size; i++ {
			validator := getValidatorOutputs[i]
			getValidatorsOutput[validator.NodeID] = validator
		}
		validatorState := &validatorstest.State{
			GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
				return getValidatorsOutput, nil
			},
		}

		b.Run(strconv.Itoa(size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_, err := GetCanonicalValidatorSetFromSubnetID(b.Context(), validatorState, pChainHeight, subnetID)
				require.NoError(b, err)
			}
		})
	}
}
