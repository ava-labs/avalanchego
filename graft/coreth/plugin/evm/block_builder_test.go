// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/plugin/evm/customtypes"
)

func TestCalculateBlockBuildingDelay(t *testing.T) {
	now := time.UnixMilli(10000)
	nowSecUint64 := uint64(now.Unix())
	nowMilliUint64 := uint64(now.UnixMilli())
	clock := &mockable.Clock{}
	clock.Set(now)
	tests := []struct {
		name                string
		currentHeader       *types.Header
		lastBuildTime       time.Time
		lastBuildParentHash common.Hash
		expectedTimeToWait  time.Duration
	}{
		{
			name: "pre_granite_returns_build_immediately_zero_time",
			currentHeader: &types.Header{
				ParentHash: common.Hash{1},
				Time:       nowSecUint64,
			},
			lastBuildTime:       time.Time{}, // Zero time means not a retry
			lastBuildParentHash: common.Hash{1},
			expectedTimeToWait:  0,
		},
		{
			name: "pre_granite_returns_build_immediately_different_parent_hash",
			currentHeader: &types.Header{
				ParentHash: common.Hash{2},
				Time:       nowSecUint64,
			},
			lastBuildTime:       now,
			lastBuildParentHash: common.Hash{1},
			expectedTimeToWait:  0,
		},
		{
			name: "pre_granite_returns_build_delays_with_same_parent_hash",
			currentHeader: &types.Header{
				ParentHash: common.Hash{1},
				Time:       nowSecUint64,
			},
			lastBuildTime:       now,
			lastBuildParentHash: common.Hash{1},
			expectedTimeToWait:  RetryDelay,
		},
		{
			name: "pre_granite_returns_build_returns_immediately_if_enough_time_passed",
			currentHeader: &types.Header{
				ParentHash: common.Hash{1},
				Time:       nowSecUint64,
			},
			lastBuildTime:       now.Add(-RetryDelay), // Less than retry delay ago
			lastBuildParentHash: common.Hash{1},       // Same as current parent
			expectedTimeToWait:  0,
		},
		{
			name: "pre_granite_returns_build_delays_only_remaining_min_delay",
			currentHeader: &types.Header{
				ParentHash: common.Hash{1},
				Time:       nowSecUint64,
			},
			lastBuildTime:       now.Add(-RetryDelay / 2), // Less than retry delay ago
			lastBuildParentHash: common.Hash{1},
			expectedTimeToWait:  RetryDelay / 2,
		},
		{
			name:                "granite_block_with_now_time",
			currentHeader:       createGraniteTestHeader(common.Hash{1}, nowMilliUint64, acp226.InitialDelayExcess),
			lastBuildTime:       time.Time{},
			lastBuildParentHash: common.Hash{1},
			expectedTimeToWait:  2000 * time.Millisecond, // should wait for initial delay
		},
		{
			name:                "granite_block_with_2_seconds_before_clock_no_retry",
			currentHeader:       createGraniteTestHeader(common.Hash{1}, nowMilliUint64-2000, acp226.InitialDelayExcess),
			lastBuildTime:       time.Time{}, // Zero time means not a retry
			lastBuildParentHash: common.Hash{1},
			expectedTimeToWait:  0, // should not wait for initial delay
		},
		{
			name:                "granite_block_with_2_seconds_before_clock_with_retry",
			currentHeader:       createGraniteTestHeader(common.Hash{1}, nowMilliUint64-2000, acp226.InitialDelayExcess),
			lastBuildTime:       now,
			lastBuildParentHash: common.Hash{1},
			expectedTimeToWait:  RetryDelay,
		},
		{
			name:                "granite_with_2_seconds_before_clock_only_waits_for_retry_delay",
			currentHeader:       createGraniteTestHeader(common.Hash{1}, nowMilliUint64-2000, 0), // 0 means min delay excess which is 1
			lastBuildTime:       now,
			lastBuildParentHash: common.Hash{1},
			expectedTimeToWait:  RetryDelay,
		},
		{
			name:                "granite_with_2_seconds_before_clock_only_waits_for_remaining_retry_delay",
			currentHeader:       createGraniteTestHeader(common.Hash{1}, nowMilliUint64-2000, 0), // 0 means min delay excess which is 1
			lastBuildTime:       now.Add(-RetryDelay / 2),                                        // Less than retry delay ago
			lastBuildParentHash: common.Hash{1},
			expectedTimeToWait:  RetryDelay / 2,
		},
		{
			name:                "granite_with_2_seconds_after_clock",
			currentHeader:       createGraniteTestHeader(common.Hash{1}, nowMilliUint64+2000, acp226.InitialDelayExcess),
			lastBuildTime:       time.Time{}, // Zero time means not a retry
			lastBuildParentHash: common.Hash{1},
			expectedTimeToWait:  4000 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &blockBuilder{
				clock: clock,
			}

			timeToWait := b.calculateBlockBuildingDelay(
				tt.lastBuildTime,
				tt.lastBuildParentHash,
				tt.currentHeader,
			)

			require.Equal(t, tt.expectedTimeToWait, timeToWait)
		})
	}
}

func createGraniteTestHeader(parentHash common.Hash, timeMilliseconds uint64, minDelayExcess acp226.DelayExcess) *types.Header {
	header := &types.Header{
		Time: timeMilliseconds / 1000,
	}
	header.ParentHash = parentHash

	extra := &customtypes.HeaderExtra{
		TimeMilliseconds: &timeMilliseconds,
		MinDelayExcess:   &minDelayExcess,
	}
	customtypes.SetHeaderExtra(header, extra)

	return header
}
