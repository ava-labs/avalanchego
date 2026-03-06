// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"fmt"
	"time"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/utils"
)

// UnmarshalTest defines a test case for JSON unmarshaling.
type UnmarshalTest[T any] struct {
	Name      string
	GivenJSON []byte
	Want      T
}

// CommonUnmarshalTests returns test cases for fields in BaseConfig.
// This is exported so implementation-specific config packages can reuse these tests.
func CommonUnmarshalTests[T any](makeConfig func(BaseConfig) T) []UnmarshalTest[T] {
	return []UnmarshalTest[T]{
		{
			Name:      "string durations parsed",
			GivenJSON: []byte(`{"api-max-duration": "1m", "continuous-profiler-frequency": "2m"}`),
			Want: makeConfig(BaseConfig{
				APIMaxDuration:              Duration{1 * time.Minute},
				ContinuousProfilerFrequency: Duration{2 * time.Minute},
			}),
		},
		{
			Name:      "integer durations parsed",
			GivenJSON: []byte(fmt.Sprintf(`{"api-max-duration": "%v", "continuous-profiler-frequency": "%v"}`, 1*time.Minute, 2*time.Minute)),
			Want: makeConfig(BaseConfig{
				APIMaxDuration:              Duration{1 * time.Minute},
				ContinuousProfilerFrequency: Duration{2 * time.Minute},
			}),
		},
		{
			Name:      "nanosecond durations parsed",
			GivenJSON: []byte(`{"api-max-duration": 5000000000, "continuous-profiler-frequency": 5000000000}`),
			Want: makeConfig(BaseConfig{
				APIMaxDuration:              Duration{5 * time.Second},
				ContinuousProfilerFrequency: Duration{5 * time.Second},
			}),
		},
		{
			Name:      "tx pool configurations",
			GivenJSON: []byte(`{"tx-pool-price-limit": 1, "tx-pool-price-bump": 2, "tx-pool-account-slots": 3, "tx-pool-global-slots": 4, "tx-pool-account-queue": 5, "tx-pool-global-queue": 6}`),
			Want: makeConfig(BaseConfig{
				TxPoolPriceLimit:   1,
				TxPoolPriceBump:    2,
				TxPoolAccountSlots: 3,
				TxPoolGlobalSlots:  4,
				TxPoolAccountQueue: 5,
				TxPoolGlobalQueue:  6,
			}),
		},
		{
			Name:      "state sync enabled",
			GivenJSON: []byte(`{"state-sync-enabled":true}`),
			Want:      makeConfig(BaseConfig{StateSyncEnabled: utils.PointerTo(true)}),
		},
		{
			Name:      "state sync sources",
			GivenJSON: []byte(`{"state-sync-ids": "NodeID-CaBYJ9kzHvrQFiYWowMkJGAQKGMJqZoat"}`),
			Want:      makeConfig(BaseConfig{StateSyncIDs: "NodeID-CaBYJ9kzHvrQFiYWowMkJGAQKGMJqZoat"}),
		},
		{
			Name:      "empty transaction history",
			GivenJSON: []byte(`{}`),
			Want:      makeConfig(BaseConfig{TransactionHistory: 0}),
		},
		{
			Name:      "zero transaction history",
			GivenJSON: []byte(`{"transaction-history": 0}`),
			Want:      makeConfig(BaseConfig{TransactionHistory: 0}),
		},
		{
			Name:      "1 transaction history",
			GivenJSON: []byte(`{"transaction-history": 1}`),
			Want:      makeConfig(BaseConfig{TransactionHistory: 1}),
		},
		{
			Name:      "allow unprotected tx hashes",
			GivenJSON: []byte(`{"allow-unprotected-tx-hashes": ["0x803351deb6d745e91545a6a3e1c0ea3e9a6a02a1a4193b70edfcd2f40f71a01c"]}`),
			Want: makeConfig(BaseConfig{
				AllowUnprotectedTxHashes: []common.Hash{common.HexToHash("0x803351deb6d745e91545a6a3e1c0ea3e9a6a02a1a4193b70edfcd2f40f71a01c")},
			}),
		},
	}
}
