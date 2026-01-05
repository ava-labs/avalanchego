// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/upgrade"

	. "github.com/ava-labs/avalanchego/snow/validators"
)

var errTest = errors.New("test error")

func TestCachedState_GetSubnetID(t *testing.T) {
	require := require.New(t)

	uncached := &validatorstest.State{
		T:               t,
		CantGetSubnetID: true,
	}
	cached := NewCachedState(uncached, upgrade.InitiallyActiveTime)

	blockchainID1 := ids.GenerateTestID()
	blockchainID2 := ids.GenerateTestID()
	subnetIDs := map[ids.ID]ids.ID{
		blockchainID1: ids.GenerateTestID(),
		blockchainID2: ids.GenerateTestID(),
	}
	tests := []struct {
		name         string
		expectCached bool
		blockchainID ids.ID
		want         ids.ID
		wantErr      error
	}{
		{
			name:         "populate_initial_entry",
			expectCached: false,
			blockchainID: blockchainID1,
			want:         subnetIDs[blockchainID1],
		},
		{
			name:         "initial_entry_was_cached",
			expectCached: true,
			blockchainID: blockchainID1,
			want:         subnetIDs[blockchainID1],
		},
		{
			name:         "cache_miss_error",
			expectCached: false,
			blockchainID: blockchainID2,
			wantErr:      errTest,
		},
		{
			name:         "cache_after_miss_error",
			expectCached: false,
			blockchainID: blockchainID2,
			want:         subnetIDs[blockchainID2],
		},
		{
			name:         "second_cache_hit",
			expectCached: true,
			blockchainID: blockchainID2,
			want:         subnetIDs[blockchainID2],
		},
		{
			name:         "cache_multiple",
			expectCached: true,
			blockchainID: blockchainID1,
			want:         subnetIDs[blockchainID1],
		},
	}
	for _, test := range tests {
		t.Logf("starting test: %s", test.name)

		var cacheMiss bool
		if !test.expectCached {
			uncached.GetSubnetIDF = func(_ context.Context, chainID ids.ID) (ids.ID, error) {
				require.Equal(test.blockchainID, chainID)
				require.False(cacheMiss)

				cacheMiss = true
				return test.want, test.wantErr
			}
		} else {
			uncached.GetSubnetIDF = nil
		}

		got, err := cached.GetSubnetID(t.Context(), test.blockchainID)
		require.ErrorIs(err, test.wantErr)
		require.Equal(test.want, got)
		require.Equal(test.expectCached, !cacheMiss)
	}
}

func TestCachedState_GetWarpValidatorSets(t *testing.T) {
	require := require.New(t)

	uncached := &validatorstest.State{
		T:                        t,
		CantGetWarpValidatorSets: true,
	}
	cached := NewCachedState(uncached, upgrade.InitiallyActiveTime)

	allVdrs0 := map[ids.ID]WarpSet{
		ids.GenerateTestID(): validatorstest.NewWarpSet(t, 3),
	}
	allVdrs1 := map[ids.ID]WarpSet{
		ids.GenerateTestID(): validatorstest.NewWarpSet(t, 3),
	}
	tests := []struct {
		name         string
		expectCached bool
		height       uint64
		want         map[ids.ID]WarpSet
		wantErr      error
	}{
		{
			name:         "populate_initial_entry",
			expectCached: false,
			height:       0,
			want:         allVdrs0,
		},
		{
			name:         "initial_entry_was_cached",
			expectCached: true,
			height:       0,
			want:         allVdrs0,
		},
		{
			name:         "cache_miss_error",
			expectCached: false,
			height:       1,
			wantErr:      errTest,
		},
		{
			name:         "cache_after_miss_error",
			expectCached: false,
			height:       1,
			want:         allVdrs1,
		},
		{
			name:         "cache_after_miss_error",
			expectCached: true,
			height:       1,
			want:         allVdrs1,
		},
		{
			name:         "cache_multiple",
			expectCached: true,
			height:       0,
			want:         allVdrs0,
		},
	}
	for _, test := range tests {
		t.Logf("starting test: %s", test.name)

		var cacheMiss bool
		if !test.expectCached {
			uncached.GetWarpValidatorSetsF = func(_ context.Context, height uint64) (map[ids.ID]WarpSet, error) {
				require.Equal(test.height, height)
				require.False(cacheMiss)

				cacheMiss = true
				return test.want, test.wantErr
			}
		} else {
			uncached.GetWarpValidatorSetsF = nil
		}

		got, err := cached.GetWarpValidatorSets(t.Context(), test.height)
		require.ErrorIs(err, test.wantErr)
		require.Equal(test.want, got)
		require.Equal(test.expectCached, !cacheMiss)
	}
}

func TestCachedState_GetWarpValidatorSet_Inactive(t *testing.T) {
	var (
		warpSet = validatorstest.NewWarpSet(t, 3)
		vdr0    = validatorstest.WarpToOutput(warpSet.Validators[0])
		vdr1    = validatorstest.WarpToOutput(warpSet.Validators[1])
		vdr2    = validatorstest.WarpToOutput(warpSet.Validators[2])
		vdrs    = map[ids.NodeID]*GetValidatorOutput{
			vdr0.NodeID: vdr0,
			vdr1.NodeID: vdr1,
			vdr2.NodeID: vdr2,
		}
	)

	tests := []struct {
		name    string
		want    WarpSet
		wantErr error
	}{
		{
			name: "success",
			want: warpSet,
		},
		{
			name:    "error",
			wantErr: errTest,
		},
	}
	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			var (
				height   = uint64(i)
				subnetID = ids.GenerateTestID()
				uncached = &validatorstest.State{
					T: t,
					GetValidatorSetF: func(_ context.Context, gotHeight uint64, gotSubnetID ids.ID) (map[ids.NodeID]*GetValidatorOutput, error) {
						require.Equal(height, gotHeight)
						require.Equal(subnetID, gotSubnetID)
						return vdrs, test.wantErr
					},
				}
				cached = NewCachedState(uncached, upgrade.UnscheduledActivationTime)
			)

			got, err := cached.GetWarpValidatorSet(t.Context(), height, subnetID)
			require.ErrorIs(err, test.wantErr)

			// Compare only the public fields, not cache fields
			require.Equal(test.want.TotalWeight, got.TotalWeight)
			require.Len(got.Validators, len(test.want.Validators))
			for i, wantVdr := range test.want.Validators {
				gotVdr := got.Validators[i]
				require.Equal(wantVdr.PublicKeyBytes, gotVdr.PublicKeyBytes)
				require.Equal(wantVdr.Weight, gotVdr.Weight)
				require.Equal(wantVdr.NodeIDs, gotVdr.NodeIDs)
			}
		})
	}
}

func TestCachedState_GetWarpValidatorSet_Active(t *testing.T) {
	require := require.New(t)

	var (
		subnetID0 = ids.GenerateTestID()
		subnetID1 = ids.GenerateTestID()
		allVdrs   = map[ids.ID]WarpSet{
			subnetID0: validatorstest.NewWarpSet(t, 3),
			subnetID1: validatorstest.NewWarpSet(t, 3),
		}
	)

	uncached := &validatorstest.State{
		T:                        t,
		CantGetWarpValidatorSets: true,
	}
	cached := NewCachedState(uncached, upgrade.InitiallyActiveTime)

	tests := []struct {
		name       string
		validators map[ids.ID]WarpSet
		height     uint64
		subnetID   ids.ID
		want       WarpSet
		wantErr    error
	}{
		{
			name:       "populate_initial_entry",
			validators: allVdrs,
			height:     0,
			subnetID:   subnetID0,
			want:       allVdrs[subnetID0],
		},
		{
			name:     "initial_entry_was_cached",
			height:   0,
			subnetID: subnetID0,
			want:     allVdrs[subnetID0],
		},
		{
			name:     "cache_miss_error",
			height:   1,
			subnetID: subnetID0,
			wantErr:  errTest,
		},
		{
			name:       "cache_after_miss_error",
			validators: allVdrs,
			height:     1,
			subnetID:   subnetID0,
			want:       allVdrs[subnetID0],
		},
		{
			name:     "cache_all_subnets",
			height:   1,
			subnetID: subnetID1,
			want:     allVdrs[subnetID1],
		},
		{
			name:     "cache_empty_subnet",
			height:   1,
			subnetID: ids.GenerateTestID(),
			want:     WarpSet{},
		},
	}
	for _, test := range tests {
		t.Logf("starting test: %s", test.name)

		var (
			cacheMiss    bool
			expectCached = test.validators == nil && test.wantErr == nil
		)
		if !expectCached {
			uncached.GetWarpValidatorSetsF = func(_ context.Context, height uint64) (map[ids.ID]WarpSet, error) {
				require.Equal(test.height, height)
				require.False(cacheMiss)

				cacheMiss = true
				return test.validators, test.wantErr
			}
		} else {
			uncached.GetWarpValidatorSetsF = nil
		}

		got, err := cached.GetWarpValidatorSet(t.Context(), test.height, test.subnetID)
		require.ErrorIs(err, test.wantErr)
		require.Equal(test.want, got)
		require.Equal(expectCached, !cacheMiss)
	}
}

func BenchmarkCachedState_GetWarpValidatorSet_Active(b *testing.B) {
	const uncachedDelay = 100 * time.Millisecond
	for size := 1; size <= 1<<10; size *= 2 {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			vdrs := validatorstest.NewWarpSet(b, uint64(size))
			allVdrs := make(map[ids.ID]WarpSet, size)
			for i := 0; i < size; i++ {
				allVdrs[ids.GenerateTestID()] = vdrs
			}

			uncached := &validatorstest.State{
				T: b,
				GetWarpValidatorSetsF: func(context.Context, uint64) (map[ids.ID]WarpSet, error) {
					time.Sleep(uncachedDelay) // Simulate uncached delay
					return allVdrs, nil
				},
			}
			cached := NewCachedState(uncached, upgrade.InitiallyActiveTime)

			ctx := b.Context()
			subnetID := ids.GenerateTestID()
			for b.Loop() {
				_, err := cached.GetWarpValidatorSet(ctx, 0, subnetID)
				require.NoError(b, err)
			}
		})
	}
}
