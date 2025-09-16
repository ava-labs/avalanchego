// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"testing"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/utils"
)

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
			header:      generateHeader(timeSeconds, utils.NewUint64(timeMillis)),
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
			header:      generateHeader(timeSeconds, utils.NewUint64(timeSeconds*1000)),
			extraConfig: extras.TestGraniteChainConfig,
		},
		{
			name:        "granite_time_milliseconds_matching_time_rounded_should_work",
			header:      generateHeader(timeSeconds, utils.NewUint64(timeMillis)),
			extraConfig: extras.TestGraniteChainConfig,
		},
		{
			name:        "granite_time_milliseconds_less_than_time_should_fail",
			header:      generateHeader(timeSeconds, utils.NewUint64((timeSeconds-1)*1000)),
			extraConfig: extras.TestGraniteChainConfig,
			expectedErr: ErrTimeMillisecondsMismatched,
		},
		{
			name:        "granite_time_milliseconds_greater_than_time_should_fail",
			header:      generateHeader(timeSeconds, utils.NewUint64((timeSeconds+1)*1000)),
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
			header: generateHeader(timeSeconds, utils.NewUint64(timeSeconds*1000)),
			parentHeader: generateHeader(
				timeSeconds+1,
				utils.NewUint64((timeSeconds+1)*1000),
			),
			extraConfig: extras.TestGraniteChainConfig,
			expectedErr: errBlockTooOld,
		},
		{
			name: "granite_time_milliseconds_earlier_than_parent_should_fail",
			header: generateHeader(
				timeSeconds,
				utils.NewUint64(timeSeconds*1000),
			),
			parentHeader: generateHeader(
				timeSeconds,
				utils.NewUint64(timeSeconds*1000+1),
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
				utils.NewUint64(uint64(now.Add(MaxFutureBlockTime).Add(1*time.Second).UnixMilli())),
			),
			extraConfig: extras.TestGraniteChainConfig,
			expectedErr: ErrBlockTooFarInFuture,
		},
		{
			name: "granite_time_milliseconds_too_far_in_future_should_fail",
			header: generateHeader(
				uint64(now.Add(MaxFutureBlockTime).Unix()),
				utils.NewUint64(uint64(now.Add(MaxFutureBlockTime).Add(1*time.Millisecond).UnixMilli())),
			),
			extraConfig: extras.TestGraniteChainConfig,
			expectedErr: ErrBlockTooFarInFuture,
		},
		{
			name:         "first_granite_block_should_work",
			header:       generateHeader(timeSeconds, utils.NewUint64(timeMillis)),
			parentHeader: generateHeader(timeSeconds, nil),
			extraConfig:  extras.TestGraniteChainConfig,
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
