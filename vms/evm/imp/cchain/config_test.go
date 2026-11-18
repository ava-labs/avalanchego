// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"testing"

	"github.com/arr4n/shed/testerr"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/firewood"
	"github.com/ava-labs/avalanchego/vms/evm/imp/vmconfig"
	"github.com/ava-labs/avalanchego/vms/evm/saedb"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
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
		name         string
		json         string
		networkID    uint32
		want         config
		wantWarnings []*loggingtest.Record
		wantErr      testerr.Want
	}{
		// Block building
		{
			name: "block_building/min_price_target",
			json: `{"min-price-target":1000}`,
			want: with(func(c *config) { c.PriceTarget = new(gas.Price(1000)) }),
		},
		{
			// An explicit 0 is a vote for the minimum, distinct from an absent
			// field, which is no vote.
			name: "block_building/min_price_target_explicit_zero",
			json: `{"min-price-target":0}`,
			want: with(func(c *config) { c.PriceTarget = new(gas.Price) }),
		},
		{
			name: "block_building/gas_target",
			json: `{"gas-target":1000}`,
			want: with(func(c *config) { c.GasTarget = new(gas.Gas(1000)) }),
		},
		{
			name: "block_building/min_delay_target",
			json: `{"min-delay-target":2000}`,
			want: with(func(c *config) { c.MinDelayTarget = new(uint64(2000)) }),
		},

		// State & trie
		{
			name:      "state/hash_commit_interval_production_network",
			json:      `{"commit-interval":256}`,
			networkID: constants.MainnetID,
			wantErr:   testerr.Is(errProductionCommitInterval),
		},
		{
			name:      "state/production_firewood",
			json:      `{"state-scheme":"firewood","commit-interval":256}`,
			networkID: constants.MainnetID,
			want: with(func(c *config) {
				c.StateScheme = firewood.Scheme
				c.CommitInterval = 256
			}),
		},

		// Warp
		{
			name: "warp/off_chain_messages",
			json: `{"warp-off-chain-messages":["0x1234"]}`,
			want: with(func(c *config) {
				c.WarpOffChainMessages = []hexutil.Bytes{{0x12, 0x34}}
			}),
		},

		// State sync
		{
			name: "state_sync_enabled",
			json: `{"state-sync-enabled":false}`,
			want: with(func(c *config) { c.StateSyncEnabled = false }),
		},

		// All active fields
		{
			name: "all_active_fields",
			json: `{
				"min-price-target":500,
				"gas-target":1500,
				"min-delay-target":3000,
				"state-sync-enabled":false,
				"warp-off-chain-messages":["0x1234"]
			}`,
			want: config{
				Config:               vmconfig.Default(),
				PriceTarget:          new(gas.Price(500)),
				GasTarget:            new(gas.Gas(1500)),
				MinDelayTarget:       new(uint64(3000)),
				StateSyncEnabled:     false,
				WarpOffChainMessages: []hexutil.Bytes{{0x12, 0x34}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Logf("parsing config:\n%s", test.json)
			log := loggingtest.NewRecorder(logging.Warn)
			snowCtx := &snow.Context{NetworkID: test.networkID, Log: log}
			got, err := parseConfig(snowCtx, []byte(test.json))
			if diff := testerr.Diff(err, test.wantErr); diff != "" {
				t.Errorf("parseConfig(...) error (-want +got)\n%s", diff)
			}
			require.Equal(t, test.want, got, "parseConfig(...)")
			require.Equal(t, test.wantWarnings, log.Records, "parseConfig(...) logs")
		})
	}
}

func TestConfigStateSyncInterval(t *testing.T) {
	tests := []struct {
		name           string
		networkID      uint32
		commitInterval uint64
		want           uint64
	}{
		{
			name:           "custom_network",
			networkID:      constants.UnitTestID,
			commitInterval: 256,
			want:           256,
		},
		{
			name:           "mainnet",
			networkID:      constants.MainnetID,
			commitInterval: 256,
			want:           saedb.DefaultCommitInterval,
		},
		{
			name:           "fuji",
			networkID:      constants.FujiID,
			commitInterval: 256,
			want:           saedb.DefaultCommitInterval,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := defaultConfig()
			c.StateScheme = firewood.Scheme
			c.CommitInterval = tt.commitInterval

			got := c.stateSyncConfig(tt.networkID).DBConfig.CommitInterval
			require.Equal(t, tt.want, got, "%T.stateSyncConfig(%d).DBConfig.CommitInterval", c, tt.networkID)
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

func TestConfigResolvePendingToLastExecuted(t *testing.T) {
	// This test only exercises plumbing of the config. A full test of
	// functionality is performed in the SAE code.

	tests := []struct {
		name    string
		opt     func(*sutConfig)
		wantErr testerr.Want
	}{
		{
			name: "default_config",
			opt:  func(*sutConfig) {},
		},
		{
			name: "explicitly_enable_mapping",
			opt: func(c *sutConfig) {
				c.vmConfig.ResolvePendingToLastExecuted = true
			},
		},
		{
			name: "explicitly_disable_mapping",
			opt: func(c *sutConfig) {
				c.vmConfig.ResolvePendingToLastExecuted = false
			},
			wantErr: testerr.Contains("pending"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := common.Address{'S', 't', 'e', 'p', 'h', 'e', 'n'}
			code := []byte{42}

			ctx, sut := newSUT(t,
				withAccount(addr, types.Account{
					Code: code,
				}),
				options.Func[sutConfig](tt.opt),
			)

			got, err := sut.EthClient().PendingCodeAt(ctx, addr)
			if diff := testerr.Diff(err, tt.wantErr); diff != "" {
				t.Fatalf("%T.PendingCodeAt(%v): %s", sut.EthClient(), addr, diff)
			}
			if tt.wantErr != nil {
				return
			}
			assert.Equalf(t, code, got, "%T.PendingCodeAt(%v)", sut.EthClient(), addr)
		})
	}
}
