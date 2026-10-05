// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fork_test

import (
	"context"
	"maps"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/fork"
	"github.com/ava-labs/avalanchego/fork/forktest"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/utils/constants"
)

func TestStaticState(t *testing.T) {
	cfg := forktest.NewConfig(t, testForkTime, ids.GenerateTestNodeID())
	s := fork.NewStaticState(cfg)

	got, err := s.GetValidatorSet(t.Context(), 12345, constants.PrimaryNetworkID)
	require.NoError(t, err, "GetValidatorSet(primary)")
	require.Equal(t, cfg.ValidatorSet(), got, "GetValidatorSet(primary)")

	got, err = s.GetValidatorSet(t.Context(), 12345, ids.GenerateTestID())
	require.NoError(t, err, "GetValidatorSet(subnet)")
	require.Empty(t, got, "GetValidatorSet(subnet)")
}

func TestState(t *testing.T) {
	const (
		forkHeight    = 100
		currentHeight = 150
	)
	var (
		cfg      = forktest.NewConfig(t, testForkTime, ids.GenerateTestNodeID())
		subnetID = ids.GenerateTestID()
		srcNode  = ids.GenerateTestNodeID()
		srcSet   = map[ids.NodeID]*validators.GetValidatorOutput{srcNode: {NodeID: srcNode, Weight: 7}}
		srcWarp  = validators.WarpSet{TotalWeight: 7}
		subWarp  = validators.WarpSet{TotalWeight: 3}
		srcCur   = map[ids.ID]*validators.GetCurrentValidatorOutput{ids.GenerateTestID(): {NodeID: srcNode, Weight: 7}}
	)
	innerWarp := map[ids.ID]validators.WarpSet{constants.PrimaryNetworkID: srcWarp, subnetID: subWarp}
	inner := &validatorstest.State{
		T: t,
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			return srcSet, nil
		},
		GetWarpValidatorSetsF: func(context.Context, uint64) (map[ids.ID]validators.WarpSet, error) {
			return innerWarp, nil
		},
		GetCurrentValidatorSetF: func(context.Context, ids.ID) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
			return srcCur, currentHeight, nil
		},
	}

	tests := []struct {
		name           string
		known          bool
		height         uint64
		subnetID       ids.ID
		wantForkedSet  bool
		wantForkedWarp bool
	}{
		{name: "H_fork unknown", height: forkHeight, subnetID: constants.PrimaryNetworkID},
		{name: "below H_fork", known: true, height: forkHeight - 1, subnetID: constants.PrimaryNetworkID},
		{name: "at H_fork", known: true, height: forkHeight, subnetID: constants.PrimaryNetworkID, wantForkedSet: true, wantForkedWarp: true},
		{name: "above H_fork", known: true, height: forkHeight + 1, subnetID: constants.PrimaryNetworkID, wantForkedSet: true, wantForkedWarp: true},
		{name: "other subnet above H_fork", known: true, height: forkHeight + 1, subnetID: subnetID, wantForkedWarp: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := fork.NewState(inner, cfg, func() (uint64, bool) { return forkHeight, tt.known })

			gotSet, err := s.GetValidatorSet(t.Context(), tt.height, tt.subnetID)
			require.NoError(t, err, "GetValidatorSet()")
			if tt.wantForkedSet {
				require.Equal(t, cfg.ValidatorSet(), gotSet, "GetValidatorSet()")
			} else {
				require.Equal(t, srcSet, gotSet, "GetValidatorSet()")
			}

			before := maps.Clone(innerWarp)
			gotWarp, err := s.GetWarpValidatorSets(t.Context(), tt.height)
			require.NoError(t, err, "GetWarpValidatorSets()")
			require.Equal(t, before, innerWarp, "GetWarpValidatorSets() must not mutate the inner map")
			require.Equal(t, subWarp, gotWarp[subnetID], "GetWarpValidatorSets()[subnet]")
			if tt.wantForkedWarp {
				require.Equal(t, cfg.WarpSet(), gotWarp[constants.PrimaryNetworkID], "GetWarpValidatorSets()[primary]")
			} else {
				require.Equal(t, srcWarp, gotWarp[constants.PrimaryNetworkID], "GetWarpValidatorSets()[primary]")
			}
		})
	}

	t.Run("current set before H_fork", func(t *testing.T) {
		s := fork.NewState(inner, cfg, func() (uint64, bool) { return forkHeight, false })
		got, height, err := s.GetCurrentValidatorSet(t.Context(), constants.PrimaryNetworkID)
		require.NoError(t, err, "GetCurrentValidatorSet()")
		require.Equal(t, srcCur, got, "GetCurrentValidatorSet() delegates when not forked")
		require.Equal(t, uint64(currentHeight), height, "GetCurrentValidatorSet() height")
	})

	t.Run("current set after H_fork", func(t *testing.T) {
		s := fork.NewState(inner, cfg, func() (uint64, bool) { return forkHeight, true })
		got, height, err := s.GetCurrentValidatorSet(t.Context(), constants.PrimaryNetworkID)
		require.NoError(t, err, "GetCurrentValidatorSet()")
		require.Equal(t, uint64(currentHeight), height, "GetCurrentValidatorSet() height")
		require.Len(t, got, 1, "GetCurrentValidatorSet()")
		for _, v := range got {
			require.Equal(t, cfg.Validators[0].NodeID, v.NodeID, "GetCurrentValidatorSet() nodeID")
			require.True(t, v.IsActive, "GetCurrentValidatorSet() IsActive")
		}
	})
}
