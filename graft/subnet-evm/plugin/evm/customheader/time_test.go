// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"testing"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
)

func TestVerifyTime(t *testing.T) {
	var (
		now         = time.Unix(1714339200, 123_456_789)
		timeSeconds = uint64(now.Unix())
		timeMillis  = uint64(now.UnixMilli())
	)
	tests := []struct {
		name         string
		header       *types.Header
		parentHeader *types.Header
		extraConfig  *extras.ChainConfig
		expectedErr  error
	}{
		{
			name:        "pre_granite_time_milliseconds_should_fail",
			header:      generateHeader(timeSeconds, utils.PointerTo(timeMillis)),
			extraConfig: extras.TestFortunaChainConfig,
			expectedErr: ErrTimeMillisecondsBeforeGranite,
		},
		{
			name:        "pre_granite_time_nil_milliseconds_should_work",
			header:      generateHeader(timeSeconds, nil),
			extraConfig: extras.TestFortunaChainConfig,
		},
		{
			name:        "granite_time_milliseconds_should_be_non_nil_and_fail",
			header:      generateHeader(timeSeconds, nil),
			extraConfig: extras.TestGraniteChainConfig,
			expectedErr: ErrTimeMillisecondsRequired,
		},
		{
			name:        "granite_time_milliseconds_matching_time_should_work",
			header:      generateHeader(timeSeconds, utils.PointerTo(timeSeconds*1000)),
			extraConfig: extras.TestGraniteChainConfig,
		},
		{
			name:        "granite_time_milliseconds_matching_time_rounded_should_work",
			header:      generateHeader(timeSeconds, utils.PointerTo(timeMillis)),
			extraConfig: extras.TestGraniteChainConfig,
		},
		{
			name:        "granite_time_milliseconds_less_than_time_should_fail",
			header:      generateHeader(timeSeconds, utils.PointerTo((timeSeconds-1)*1000)),
			extraConfig: extras.TestGraniteChainConfig,
			expectedErr: ErrTimeMillisecondsMismatched,
		},
		{
			name:        "granite_time_milliseconds_greater_than_time_should_fail",
			header:      generateHeader(timeSeconds, utils.PointerTo((timeSeconds+1)*1000)),
			extraConfig: extras.TestGraniteChainConfig,
			expectedErr: ErrTimeMillisecondsMismatched,
		},
		{
			name:         "pre_granite_time_earlier_than_parent_should_fail",
			header:       generateHeader(timeSeconds, nil),
			parentHeader: generateHeader(timeSeconds+1, nil),
			extraConfig:  extras.TestFortunaChainConfig,
			expectedErr:  errBlockTooOld,
		},
		{
			name:   "granite_time_earlier_than_parent_should_fail",
			header: generateHeader(timeSeconds, utils.PointerTo(timeSeconds*1000)),
			parentHeader: generateHeader(
				timeSeconds+1,
				utils.PointerTo((timeSeconds+1)*1000),
			),
			extraConfig: extras.TestGraniteChainConfig,
			expectedErr: errBlockTooOld,
		},
		{
			name: "granite_time_milliseconds_earlier_than_parent_should_fail",
			header: generateHeader(
				timeSeconds,
				utils.PointerTo(timeSeconds*1000),
			),
			parentHeader: generateHeader(
				timeSeconds,
				utils.PointerTo(timeSeconds*1000+1),
			),
			extraConfig: extras.TestGraniteChainConfig,
			expectedErr: errBlockTooOld,
		},
		{
			name:        "pre_granite_time_too_far_in_future_should_fail",
			header:      generateHeader(uint64(now.Add(MaxFutureBlockTime).Add(1*time.Second).Unix()), nil),
			extraConfig: extras.TestFortunaChainConfig,
			expectedErr: ErrBlockTooFarInFuture,
		},
		{
			name: "granite_time_too_far_in_future_should_fail",
			header: generateHeader(
				uint64(now.Add(MaxFutureBlockTime).Add(1*time.Second).Unix()),
				utils.PointerTo(uint64(now.Add(MaxFutureBlockTime).Add(1*time.Second).UnixMilli())),
			),
			extraConfig: extras.TestGraniteChainConfig,
			expectedErr: ErrBlockTooFarInFuture,
		},
		{
			name: "granite_time_milliseconds_too_far_in_future_should_fail",
			header: generateHeader(
				uint64(now.Add(MaxFutureBlockTime).Unix()),
				utils.PointerTo(uint64(now.Add(MaxFutureBlockTime).Add(1*time.Millisecond).UnixMilli())),
			),
			extraConfig: extras.TestGraniteChainConfig,
			expectedErr: ErrBlockTooFarInFuture,
		},
		{
			name:         "first_granite_block_should_work",
			header:       generateHeader(timeSeconds, utils.PointerTo(timeMillis)),
			parentHeader: generateHeader(timeSeconds, nil),
			extraConfig:  extras.TestGraniteChainConfig,
		},
		// Min delay verification tests
		{
			name:         "pre_granite_no_min_delay_verification",
			header:       generateHeader(timeSeconds, nil),
			parentHeader: generateHeader(timeSeconds, nil),
			extraConfig:  extras.TestFortunaChainConfig,
		},
		{
			name: "granite_first_block_no_parent_min_delay_excess",
			header: generateHeaderWithMinDelayExcessAndTime(
				timeSeconds,
				utils.PointerTo(timeMillis),
				utils.PointerTo(acp226.InitialDelayExcess),
			),
			parentHeader: generateHeader(timeSeconds-1, nil), // Pre-Granite parent
			extraConfig:  extras.TestGraniteChainConfig,
		},
		{
			name: "granite_initial_delay_met",
			header: generateHeaderWithMinDelayExcessAndTime(
				timeSeconds,
				utils.PointerTo(timeMillis),
				utils.PointerTo(acp226.InitialDelayExcess),
			),
			parentHeader: generateHeaderWithMinDelayExcessAndTime(
				timeSeconds-1,
				utils.PointerTo(timeMillis-2000), // 2000 ms is the exact initial delay
				utils.PointerTo(acp226.InitialDelayExcess),
			),
			extraConfig: extras.TestGraniteChainConfig,
		},
		{
			name: "granite_initial_delay_not_met",
			header: generateHeaderWithMinDelayExcessAndTime(
				timeSeconds,
				utils.PointerTo(timeMillis),
				utils.PointerTo(acp226.InitialDelayExcess),
			),
			parentHeader: generateHeaderWithMinDelayExcessAndTime(
				timeSeconds-1,
				utils.PointerTo(timeMillis-1999), // 1 ms less than required
				utils.PointerTo(acp226.InitialDelayExcess),
			),
			extraConfig: extras.TestGraniteChainConfig,
			expectedErr: ErrMinDelayNotMet,
		},
		{
			name: "granite_future_timestamp_within_limits",
			header: generateHeaderWithMinDelayExcessAndTime(
				timeSeconds+5, // 5 seconds in future
				utils.PointerTo(timeMillis+5000),
				utils.PointerTo(acp226.InitialDelayExcess),
			),
			parentHeader: generateHeaderWithMinDelayExcessAndTime(
				timeSeconds-1,
				utils.PointerTo(timeMillis-2000),
				utils.PointerTo(acp226.InitialDelayExcess),
			),
			extraConfig: extras.TestGraniteChainConfig,
		},
		{
			name: "granite_future_timestamp_abuse",
			header: generateHeaderWithMinDelayExcessAndTime(
				timeSeconds+15, // 15 seconds in future, exceeds MaxFutureBlockTime
				utils.PointerTo(timeMillis+15000),
				utils.PointerTo(acp226.InitialDelayExcess),
			),
			parentHeader: generateHeaderWithMinDelayExcessAndTime(
				timeSeconds-1,
				utils.PointerTo(timeMillis-2000),
				utils.PointerTo(acp226.InitialDelayExcess),
			),
			extraConfig: extras.TestGraniteChainConfig,
			expectedErr: ErrBlockTooFarInFuture,
		},
		{
			name: "granite_zero_delay_excess",
			header: generateHeaderWithMinDelayExcessAndTime(
				timeSeconds,
				utils.PointerTo(timeMillis),
				utils.PointerTo(acp226.DelayExcess(0)),
			),
			parentHeader: generateHeaderWithMinDelayExcessAndTime(
				timeSeconds,
				utils.PointerTo(timeMillis-1),          // 1ms delay, meets zero requirement
				utils.PointerTo(acp226.DelayExcess(0)), // Parent has zero delay excess
			),
			extraConfig: extras.TestGraniteChainConfig,
		},
		{
			name: "granite_zero_delay_excess_but_zero_delay",
			header: generateHeaderWithMinDelayExcessAndTime(
				timeSeconds,
				utils.PointerTo(timeMillis),
				utils.PointerTo(acp226.DelayExcess(0)),
			),
			parentHeader: generateHeaderWithMinDelayExcessAndTime(
				timeSeconds,
				utils.PointerTo(timeMillis),            // Same timestamp, zero delay
				utils.PointerTo(acp226.DelayExcess(0)), // Parent has zero delay excess
			),
			extraConfig: extras.TestGraniteChainConfig,
			expectedErr: ErrMinDelayNotMet,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Unless the parentHeader is explicitly set, make it a copy of the header.
			// parentHeader == header will pass if and only if header is correct.
			parentHeader := test.parentHeader
			if test.parentHeader == nil {
				parentHeader = types.CopyHeader(test.header)
			}
			err := VerifyTime(test.extraConfig, parentHeader, test.header, now)
			require.ErrorIs(t, err, test.expectedErr)
		})
	}
}

func TestGetNextTimestamp(t *testing.T) {
	now := time.Unix(1714339200, 123_456_789) // Fixed timestamp for consistent testing
	nowSeconds := uint64(now.Unix())
	nowMillis := uint64(now.UnixMilli())

	tests := []struct {
		name           string
		parent         *types.Header
		now            time.Time
		expectedSec    uint64
		expectedMillis uint64
	}{
		{
			name:           "current_time_after_parent_time_no_milliseconds",
			parent:         generateHeader(nowSeconds-10, nil),
			now:            now,
			expectedSec:    nowSeconds,
			expectedMillis: nowMillis,
		},
		{
			name:           "current_time_after_parent_time_with_milliseconds",
			parent:         generateHeader(nowSeconds-10, utils.PointerTo(nowMillis-500)),
			now:            now,
			expectedSec:    nowSeconds,
			expectedMillis: nowMillis,
		},
		{
			name:           "current_time_equals_parent_time_no_milliseconds",
			parent:         generateHeader(nowSeconds, nil),
			now:            now,
			expectedSec:    nowSeconds,
			expectedMillis: nowSeconds * 1000, // parent.Time * 1000
		},
		{
			name:           "current_time_before_parent_time_no_milliseconds",
			parent:         generateHeader(nowSeconds+10, nil),
			now:            now,
			expectedSec:    nowSeconds + 10,
			expectedMillis: (nowSeconds + 10) * 1000, // parent.Time * 1000
		},
		{
			name:           "current_time_before_parent_time_with_milliseconds",
			parent:         generateHeader(nowSeconds+10, utils.PointerTo(nowMillis)),
			now:            now,
			expectedSec:    nowSeconds,
			expectedMillis: nowMillis,
		},
		{
			name:           "current_time_milliseconds_before_parent_time_milliseconds",
			parent:         generateHeader(nowSeconds, utils.PointerTo(nowMillis+10)),
			now:            now,
			expectedSec:    nowSeconds,
			expectedMillis: nowMillis,
		},
		{
			name:           "current_time_equals_parent_time_with_milliseconds_granite",
			parent:         generateHeader(nowSeconds, utils.PointerTo(nowMillis)),
			now:            now,
			expectedSec:    nowSeconds,
			expectedMillis: nowMillis,
		},
		{
			name:           "current_timesec_equals_parent_time_with_less_milliseconds",
			parent:         generateHeader(nowSeconds, utils.PointerTo(nowMillis-10)),
			now:            now,
			expectedSec:    nowSeconds,
			expectedMillis: nowMillis,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			time := GetNextTimestamp(test.parent, test.now)
			require.Equal(t, test.expectedSec, uint64(time.Unix()))
			require.Equal(t, test.expectedMillis, uint64(time.UnixMilli()))
		})
	}
}

func generateHeader(timeSeconds uint64, timeMilliseconds *uint64) *types.Header {
	return customtypes.WithHeaderExtra(
		&types.Header{
			Time: timeSeconds,
		},
		&customtypes.HeaderExtra{
			TimeMilliseconds: timeMilliseconds,
		},
	)
}

func generateHeaderWithMinDelayExcessAndTime(timeSeconds uint64, timeMilliseconds *uint64, minDelayExcess *acp226.DelayExcess) *types.Header {
	return customtypes.WithHeaderExtra(
		&types.Header{
			Time: timeSeconds,
		},
		&customtypes.HeaderExtra{
			TimeMilliseconds: timeMilliseconds,
			MinDelayExcess:   minDelayExcess,
		},
	)
}
