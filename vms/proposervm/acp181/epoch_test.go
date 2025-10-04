// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp181

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"

	statelessblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

func TestEpoch(t *testing.T) {
	var (
		now            = time.Now().Truncate(time.Second)
		nowPlusEpoch   = now.Add(upgrade.Default.GraniteEpochDuration)
		nowPlus2Epochs = now.Add(2 * upgrade.Default.GraniteEpochDuration)
		nowPlus3Epochs = now.Add(2 * upgrade.Default.GraniteEpochDuration)
	)

	tests := []struct {
		name               string
		fork               upgradetest.Fork
		parentPChainHeight uint64
		parentEpoch        statelessblock.Epoch
		parentTimestamp    time.Time
		childTimestamp     time.Time
		expected           statelessblock.Epoch
	}{
		{
			name:               "pre_granite",
			fork:               upgradetest.NoUpgrades,
			parentPChainHeight: 100,
			parentTimestamp:    now,
			childTimestamp:     now,
			expected:           statelessblock.Epoch{},
		},
		{
			name:               "first_post_granite_epoch",
			fork:               upgradetest.Latest,
			parentPChainHeight: 100,
			parentTimestamp:    now,
			childTimestamp:     now.Add(time.Second),
			expected: statelessblock.Epoch{
				PChainHeight: 100,
				Number:       1,
				StartTime:    now.Unix(),
			},
		},
		{
			name:               "keep_same_epoch",
			fork:               upgradetest.Latest,
			parentPChainHeight: 101,
			parentEpoch: statelessblock.Epoch{
				PChainHeight: 100,
				Number:       1,
				StartTime:    now.Unix(),
			},
			parentTimestamp: now.Add(upgrade.Default.GraniteEpochDuration / 2),
			childTimestamp:  nowPlusEpoch,
			expected: statelessblock.Epoch{
				PChainHeight: 100,
				Number:       1,
				StartTime:    now.Unix(),
			},
		},
		{
			name:               "barely_transition_to_next_epoch",
			fork:               upgradetest.Latest,
			parentPChainHeight: 101,
			parentEpoch: statelessblock.Epoch{
				PChainHeight: 100,
				Number:       1,
				StartTime:    now.Unix(),
			},
			parentTimestamp: nowPlusEpoch,
			childTimestamp:  nowPlusEpoch,
			expected: statelessblock.Epoch{
				PChainHeight: 101,
				Number:       2,
				StartTime:    nowPlusEpoch.Unix(),
			},
		},
		{
			name:               "transition_to_next_epoch",
			fork:               upgradetest.Latest,
			parentPChainHeight: 101,
			parentEpoch: statelessblock.Epoch{
				PChainHeight: 100,
				Number:       1,
				StartTime:    now.Unix(),
			},
			parentTimestamp: nowPlus2Epochs,
			childTimestamp:  nowPlus3Epochs,
			expected: statelessblock.Epoch{
				PChainHeight: 101,
				Number:       2,
				StartTime:    nowPlus2Epochs.Unix(),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			epoch := Epoch(
				upgradetest.GetConfig(test.fork),
				test.parentPChainHeight,
				test.parentEpoch,
				test.parentTimestamp,
				test.childTimestamp,
			)
			require.Equal(t, test.expected, epoch)
		})
	}
}
