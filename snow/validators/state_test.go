// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"

	. "github.com/ava-labs/avalanchego/snow/validators"
)

var errTest = errors.New("test error")

func TestCachedState_GetSubnetID(t *testing.T) {
	require := require.New(t)

	uncached := &validatorstest.State{
		T:               t,
		CantGetSubnetID: true,
	}
	cached := NewCachedState(uncached)

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
	cached := NewCachedState(uncached)

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
