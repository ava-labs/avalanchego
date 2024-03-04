// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bag"
	"github.com/stretchr/testify/require"
)

type step struct {
	votes            bag.Bag[ids.ID]
	expectFinalized  bool
	expectPreference ids.ID
}

func consensusTest(require *require.Assertions, sf Consensus, steps []step) {
	for _, step := range steps {
		// Note: it would be nice to switch from using a bag to a "Moder" interface, so that specifying only
		// the mode in a given "step" does not hide any information.
		// Leaving this to call only RecordPoll for snowflake+ tests for now.
		sf.RecordPoll(step.votes)
		require.Equal(step.expectFinalized, sf.Finalized())
		require.Equal(step.expectPreference, sf.Preference())
	}
}

func TestSnowflakePlusLowestAlphaThreshold(t *testing.T) {
	k := 3
	alpha1 := k/2 + 1
	alpha2Cutoff := alpha1
	betas := []int{2, 1}
	// Create a new snowflake+ instance
	sf := newSnowflakePlus(alpha1, alpha2Cutoff, betas, Red)
	consensusTest(require.New(t), sf, []step{
		{
			votes:            bag.Of(Red, Red, Blue),
			expectFinalized:  false,
			expectPreference: Red,
		},
		{
			votes:            bag.Of(Red, Red, Blue),
			expectFinalized:  true,
			expectPreference: Red,
		},
	})
}

func TestSnowflakePlusHighestAlphaThreshold(t *testing.T) {
	k := 3
	alpha1 := k/2 + 1
	alpha2Cutoff := alpha1
	betas := []int{2, 1}
	// Create a new snowflake+ instance
	sf := newSnowflakePlus(alpha1, alpha2Cutoff, betas, Red)
	consensusTest(require.New(t), sf, []step{
		{
			votes:            bag.Of(Red, Red, Red),
			expectFinalized:  true,
			expectPreference: Red,
		},
	})
}

func TestSnowflakePlusFlips(t *testing.T) {
	k := 3
	alpha1 := k/2 + 1
	alpha2Cutoff := alpha1
	betas := []int{2, 1}
	// Create a new snowflake+ instance
	sf := newSnowflakePlus(alpha1, alpha2Cutoff, betas, Red)
	consensusTest(require.New(t), sf, []step{
		{
			votes:            bag.Of(Red, Red, Blue),
			expectFinalized:  false,
			expectPreference: Red,
		},
		{
			votes:            bag.Of(Red, Blue, Blue),
			expectFinalized:  false,
			expectPreference: Blue,
		},
		{
			votes:            bag.Of(Red, Red, Blue),
			expectFinalized:  false,
			expectPreference: Red,
		},
		{
			votes:            bag.Of(Red, Red, Blue),
			expectFinalized:  true,
			expectPreference: Red,
		},
	})
}
