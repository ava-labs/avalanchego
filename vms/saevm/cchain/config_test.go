// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"encoding/json"
	"testing"

	"github.com/arr4n/shed/testerr"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/saevm/sae/rpc"
)

func TestParseConfig(t *testing.T) {
	// with applies mod to defaultConfig() so each override case asserts that its
	// JSON field overrides the default while the rest are preserved.
	with := func(mod func(*config)) config {
		c := defaultConfig()
		mod(&c)
		return c
	}

	tests := []struct {
		name      string
		json      string
		networkID uint32
		want      config
		wantErr   testerr.Want
	}{
		{
			name:    "invalid_json",
			json:    "invalid",
			wantErr: errIsType[*json.SyntaxError](),
		},
		{
			name: "empty_input",
			want: defaultConfig(),
		},
		{
			name: "empty_object",
			json: `{}`,
			want: defaultConfig(),
		},
		{
			name: "pruning_disabled",
			json: `{"pruning-enabled":false}`,
			want: with(func(c *config) { c.Pruning = false }),
		},
		{
			name: "local_txs_enabled",
			json: `{"local-txs-enabled":true}`,
			want: with(func(c *config) { c.LocalTxsEnabled = true }),
		},
		{
			name: "tx_pool_slots",
			json: `{"tx-pool-account-slots":32,"tx-pool-global-slots":9000}`,
			want: with(func(c *config) {
				c.TxPoolAccountSlots = 32
				c.TxPoolGlobalSlots = 9000
			}),
		},
		{
			name: "price_target",
			json: `{"min-price-target":1000}`,
			want: with(func(c *config) { c.PriceTarget = utils.PointerTo(gas.Price(1000)) }),
		},
		{
			// An explicit 0 is a vote for the minimum, distinct from an absent
			// field, which is no vote.
			name: "explicit_zero",
			json: `{"min-price-target":0}`,
			want: with(func(c *config) { c.PriceTarget = utils.PointerTo(gas.Price(0)) }),
		},
		{
			name: "allow_unprotected_txs",
			json: `{"allow-unprotected-txs":true}`,
			want: with(func(c *config) { c.AllowUnprotectedTxs = true }),
		},
		{
			name: "gas_target",
			json: `{"gas-target":1000}`,
			want: with(func(c *config) { c.GasTarget = utils.PointerTo(gas.Gas(1000)) }),
		},
		{
			name: "batch_request_limit",
			json: `{"batch-request-limit":50}`,
			want: with(func(c *config) { c.BatchRequestLimit = 50 }),
		},
		{
			name: "batch_request_limit_explicit_zero",
			json: `{"batch-request-limit":0}`, // 0 disables the batch limit
			want: with(func(c *config) { c.BatchRequestLimit = 0 }),
		},
		{
			name:    "batch_request_limit_too_large",
			json:    `{"batch-request-limit":9223372036854775808}`, // math.MaxInt64 + 1
			wantErr: testerr.Is(rpc.ErrBatchRequestLimitTooLarge),
		},
		{
			name: "min_delay_target",
			json: `{"min-delay-target":2000}`,
			want: with(func(c *config) { c.MinDelayTarget = utils.PointerTo[uint64](2000) }),
		},
		{
			name: "warp_off_chain_messages",
			json: `{"warp-off-chain-messages":["0x1234"]}`,
			want: with(func(c *config) {
				c.WarpOffChainMessages = []hexutil.Bytes{{0x12, 0x34}}
			}),
		},
		{
			name:      "commit_interval_production_network",
			json:      `{"commit-interval":256}`,
			networkID: constants.MainnetID,
			wantErr:   testerr.Is(errProductionCommitInterval),
		},
		{
			name:      "commit_interval_non_production_network",
			json:      `{"commit-interval":256}`,
			networkID: constants.UnitTestID,
			want:      with(func(c *config) { c.CommitInterval = 256 }),
		},
		{
			name: "trie_clean_cache",
			json: `{"trie-clean-cache":256}`,
			want: with(func(c *config) { c.TrieCleanCache = 256 }),
		},
		{
			name: "snapshot_cache",
			json: `{"snapshot-cache":128}`,
			want: with(func(c *config) { c.SnapshotCache = 128 }),
		},
		{
			name: "all_active_fields",
			json: `{
				"min-price-target":500,
				"gas-target":1500,
				"min-delay-target":3000,
				"pruning-enabled":false,
				"local-txs-enabled":true,
				"tx-pool-account-slots":8,
				"tx-pool-global-slots":2048,
				"allow-unprotected-txs":true,
				"batch-request-limit":50,
				"warp-off-chain-messages":["0x1234"],
				"trie-clean-cache":256,
				"snapshot-cache":128,
				"commit-interval":256
			}`,
			want: config{
				PriceTarget:          utils.PointerTo(gas.Price(500)),
				GasTarget:            utils.PointerTo(gas.Gas(1500)),
				MinDelayTarget:       utils.PointerTo[uint64](3000),
				Pruning:              false,
				CommitInterval:       256,
				LocalTxsEnabled:      true,
				TxPoolAccountSlots:   8,
				TxPoolGlobalSlots:    2048,
				AllowUnprotectedTxs:  true,
				BatchRequestLimit:    50,
				WarpOffChainMessages: []hexutil.Bytes{{0x12, 0x34}},
				TrieCleanCache:       256,
				SnapshotCache:        128,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Logf("parsing config:\n%s", test.json)
			got, err := parseConfig([]byte(test.json), test.networkID)
			if diff := testerr.Diff(err, test.wantErr); diff != "" {
				t.Errorf("parseConfig(...) error (-want +got)\n%s", diff)
			}
			require.Equal(t, test.want, got, "parseConfig(...)")
		})
	}
}

func TestConfig_WarpMessages(t *testing.T) {
	payload, err := payload.NewAddressedCall(
		utils.RandomBytes(20),
		[]byte("test"),
	)
	require.NoError(t, err, "payload.NewAddressedCall(...)")

	msg, err := warp.NewUnsignedMessage(constants.UnitTestID, ids.GenerateTestID(), payload.Bytes())
	require.NoError(t, err, "warp.NewUnsignedMessage(...)")

	tests := []struct {
		name    string
		bytes   []hexutil.Bytes
		want    []*warp.UnsignedMessage
		wantErr error
	}{
		{
			name: "empty",
			want: []*warp.UnsignedMessage{},
		},
		{
			name:  "single_message",
			bytes: []hexutil.Bytes{msg.Bytes()},
			want:  []*warp.UnsignedMessage{msg},
		},
		{
			name:  "multiple_messages",
			bytes: []hexutil.Bytes{msg.Bytes(), msg.Bytes()},
			want:  []*warp.UnsignedMessage{msg, msg},
		},
		{
			name:    "invalid_message",
			bytes:   []hexutil.Bytes{{0xff}},
			wantErr: errParsingWarpMessage,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := config{
				WarpOffChainMessages: test.bytes,
			}

			got, err := c.WarpMessages()
			require.ErrorIsf(t, err, test.wantErr, "%T.WarpMessages()", c)
			require.Equalf(t, test.want, got, "%T.WarpMessages()", c)
		})
	}
}
