// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

func TestUnmarshalConfig(t *testing.T) {
	tests := []struct {
		name        string
		givenJSON   []byte
		expected    Config
		expectedErr bool
	}{
		{
			"string durations parsed",
			[]byte(`{"api-max-duration": "1m", "continuous-profiler-frequency": "2m", "tx-pool-rejournal": "3m30s"}`),
			Config{APIMaxDuration: Duration{1 * time.Minute}, ContinuousProfilerFrequency: Duration{2 * time.Minute}, TxPoolRejournal: Duration{3*time.Minute + 30*time.Second}},
			false,
		},
		{
			"integer durations parsed",
			[]byte(fmt.Sprintf(`{"api-max-duration": "%v", "continuous-profiler-frequency": "%v"}`, 1*time.Minute, 2*time.Minute)),
			Config{APIMaxDuration: Duration{1 * time.Minute}, ContinuousProfilerFrequency: Duration{2 * time.Minute}},
			false,
		},
		{
			"nanosecond durations parsed",
			[]byte(`{"api-max-duration": 5000000000, "continuous-profiler-frequency": 5000000000, "tx-pool-rejournal": 9000000000}`),
			Config{APIMaxDuration: Duration{5 * time.Second}, ContinuousProfilerFrequency: Duration{5 * time.Second}, TxPoolRejournal: Duration{9 * time.Second}},
			false,
		},
		{
			"bad durations",
			[]byte(`{"api-max-duration": "bad-duration"}`),
			Config{},
			true,
		},

		{
			"tx pool configurations",
			[]byte(`{"tx-pool-journal": "hello", "tx-pool-price-limit": 1, "tx-pool-price-bump": 2, "tx-pool-account-slots": 3, "tx-pool-global-slots": 4, "tx-pool-account-queue": 5, "tx-pool-global-queue": 6}`),
			Config{
				TxPoolJournal:      "hello",
				TxPoolPriceLimit:   1,
				TxPoolPriceBump:    2,
				TxPoolAccountSlots: 3,
				TxPoolGlobalSlots:  4,
				TxPoolAccountQueue: 5,
				TxPoolGlobalQueue:  6,
			},
			false,
		},

		{
			"state sync enabled",
			[]byte(`{"state-sync-enabled":true}`),
			Config{StateSyncEnabled: true},
			false,
		},
		{
			"state sync sources",
			[]byte(`{"state-sync-ids": "NodeID-CaBYJ9kzHvrQFiYWowMkJGAQKGMJqZoat"}`),
			Config{StateSyncIDs: "NodeID-CaBYJ9kzHvrQFiYWowMkJGAQKGMJqZoat"},
			false,
		},
		{
			"empty tx lookup limit",
			[]byte(`{}`),
			Config{TxLookupLimit: 0},
			false,
		},
		{
			"zero tx lookup limit",
			[]byte(`{"tx-lookup-limit": 0}`),
			func() Config {
				return Config{TxLookupLimit: 0}
			}(),
			false,
		},
		{
			"1 tx lookup limit",
			[]byte(`{"tx-lookup-limit": 1}`),
			func() Config {
				return Config{TxLookupLimit: 1}
			}(),
			false,
		},
		{
			"-1 tx lookup limit",
			[]byte(`{"tx-lookup-limit": -1}`),
			Config{},
			true,
		},
		{
			"allow unprotected tx hashes",
			[]byte(`{"allow-unprotected-tx-hashes": ["0x803351deb6d745e91545a6a3e1c0ea3e9a6a02a1a4193b70edfcd2f40f71a01c"]}`),
			Config{AllowUnprotectedTxHashes: []common.Hash{common.HexToHash("0x803351deb6d745e91545a6a3e1c0ea3e9a6a02a1a4193b70edfcd2f40f71a01c")}},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tmp Config
			err := json.Unmarshal(tt.givenJSON, &tmp)
			if tt.expectedErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, tmp)
			}
		})
	}
}
