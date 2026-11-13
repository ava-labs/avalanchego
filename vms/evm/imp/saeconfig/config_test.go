// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saeconfig

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/arr4n/shed/testerr"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/firewood"
	"github.com/ava-labs/avalanchego/vms/evm/sae/rpc"
	"github.com/ava-labs/avalanchego/vms/evm/saedb"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

// with applies mod to [Default] so each override case asserts that its field
// overrides the default while the rest are preserved.
func with(mod func(*Config)) Config {
	c := Default()
	mod(&c)
	return c
}

func TestParseConfig(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()

	tests := []struct {
		name         string
		json         string
		want         Config
		wantWarnings []*loggingtest.Record
		wantErr      testerr.Want
	}{
		// Defaults and errors
		{
			name:    "defaults/invalid_json",
			json:    "invalid",
			wantErr: testerr.As(func(*json.SyntaxError) string { return "" }),
		},
		{
			name: "defaults/empty_input",
			want: Default(),
		},
		{
			name: "defaults/empty_object",
			json: `{}`,
			want: Default(),
		},

		// Block building
		{
			name: "block_building/min_price_target",
			json: `{"min-price-target":1000}`,
			want: with(func(c *Config) { c.PriceTarget = new(gas.Price(1000)) }),
		},
		{
			// An explicit 0 is a vote for the minimum, distinct from an absent
			// field, which is no vote.
			name: "block_building/min_price_target_explicit_zero",
			json: `{"min-price-target":0}`,
			want: with(func(c *Config) { c.PriceTarget = new(gas.Price) }),
		},
		{
			name: "block_building/gas_target",
			json: `{"gas-target":1000}`,
			want: with(func(c *Config) { c.GasTarget = new(gas.Gas(1000)) }),
		},
		{
			name: "block_building/min_delay_target",
			json: `{"min-delay-target":2000}`,
			want: with(func(c *Config) { c.MinDelayTarget = new(uint64(2000)) }),
		},

		// State & trie
		{
			name: "state/pruning_disabled",
			json: `{"pruning-enabled":false}`,
			want: with(func(c *Config) { c.Pruning = false }),
		},
		{
			name: "state_scheme",
			json: `{"state-scheme":"firewood"}`,
			want: with(func(c *Config) { c.StateScheme = firewood.Scheme }),
		},
		{
			name: "state/commit_interval",
			json: `{"commit-interval":256}`,
			want: with(func(c *Config) { c.CommitInterval = 256 }),
		},
		{
			name: "state/trie_clean_cache",
			json: `{"trie-clean-cache":256}`,
			want: with(func(c *Config) { c.TrieCleanCache = 256 }),
		},
		{
			name: "state/snapshot_cache",
			json: `{"snapshot-cache":128}`,
			want: with(func(c *Config) { c.SnapshotCache = 128 }),
		},
		{
			name: "state/allow_missing_tries",
			json: `{"allow-missing-tries":true}`,
			want: with(func(c *Config) { c.AllowMissingTries = true }),
		},

		// Transaction pool
		{
			name: "tx_pool/local_txs_enabled",
			json: `{"local-txs-enabled":true}`,
			want: with(func(c *Config) { c.LocalTxsEnabled = true }),
		},
		{
			name: "tx_pool/slots",
			json: `{"tx-pool-account-slots":32,"tx-pool-global-slots":9000}`,
			want: with(func(c *Config) {
				c.TxPoolAccountSlots = 32
				c.TxPoolGlobalSlots = 9000
			}),
		},

		// APIs
		{
			name: "api/apis",
			json: `{"apis":["subscription","db","net","web3"]}`,
			want: with(func(c *Config) {
				c.APIs = set.Of(rpc.APISubscription, rpc.APIDB, rpc.APINet, rpc.APIWeb3)
			}),
		},
		{
			name: "api/apis_explicit_empty",
			json: `{"apis":[]}`,
			want: with(func(c *Config) { c.APIs.Clear() }),
		},
		{
			name: "api/allow_unprotected_txs",
			json: `{"allow-unprotected-txs":true}`,
			want: with(func(c *Config) { c.AllowUnprotectedTxs = true }),
		},
		{
			name: "api/max_blocks_per_request",
			json: `{"api-max-blocks-per-request":2000}`,
			want: with(func(c *Config) { c.MaxBlocksPerRequest = 2000 }),
		},
		{
			name: "api/batch_request_limit",
			json: `{"batch-request-limit":50}`,
			want: with(func(c *Config) { c.BatchRequestLimit = 50 }),
		},
		{
			name: "api/batch_request_limit_explicit_zero",
			json: `{"batch-request-limit":0}`, // 0 disables the batch limit
			want: with(func(c *Config) { c.BatchRequestLimit = 0 }),
		},
		{
			name: "api/api_max_duration",
			json: `{"api-max-duration":"1m"}`,
			want: with(func(c *Config) { c.APIMaxDuration = Duration{Duration: time.Minute} }),
		},
		{
			name:    "api/api_max_duration_number",
			json:    `{"api-max-duration":5000000000}`,
			wantErr: testerr.As(func(*json.UnmarshalTypeError) string { return "" }),
		},
		{
			name:    "api/api_max_duration_unparseable",
			json:    `{"api-max-duration":"not-a-duration"}`,
			wantErr: testerr.Contains("invalid duration"),
		},
		{
			name: "api/enable_map_pending_to_last_executed",
			json: `{"api-resolve-pending-to-last-executed":true}`,
			want: with(func(c *Config) { c.ResolvePendingToLastExecuted = true }),
		},
		{
			name: "api/disable_map_pending_to_last_executed",
			json: `{"api-resolve-pending-to-last-executed":false}`,
			want: with(func(c *Config) { c.ResolvePendingToLastExecuted = false }),
		},

		// Warp
		{
			name: "warp/off_chain_messages",
			json: `{"warp-off-chain-messages":["0x1234"]}`,
			want: with(func(c *Config) {
				c.WarpOffChainMessages = []hexutil.Bytes{{0x12, 0x34}}
			}),
		},

		// State sync
		{
			name: "state_sync_enabled",
			json: `{"state-sync-enabled":false}`,
			want: with(func(c *Config) { c.StateSyncEnabled = false }),
		},

		// Internal
		{
			name: "internal/state_sync_ids",
			json: fmt.Sprintf(`{"state-sync-ids":["%s"]}`, nodeID),
			want: with(func(c *Config) {
				c.StateSyncIDs = set.Of(nodeID)
			}),
		},

		// Unrecognized options
		{
			name: "unrecognized/options",
			json: `{"rpc-gas-cap":50,"hello austin larson":1,"trie-clean-cache":256}`,
			want: with(func(c *Config) { c.TrieCleanCache = 256 }),
			wantWarnings: []*loggingtest.Record{{
				Level:  logging.Warn,
				Msg:    "ignoring unrecognized config options",
				Fields: []zap.Field{zap.Strings("options", []string{"hello austin larson", "rpc-gas-cap"})},
			}},
		},

		// All active fields
		{
			name: "all_active_fields",
			json: `{
				"min-price-target":500,
				"gas-target":1500,
				"min-delay-target":3000,
				"pruning-enabled":false,
				"state-scheme":"firewood",
				"commit-interval":256,
				"trie-clean-cache":256,
				"snapshot-cache":128,
				"allow-missing-tries":true,
				"local-txs-enabled":true,
				"tx-pool-account-slots":8,
				"tx-pool-global-slots":2048,
				"apis":["chain","trace"],
				"allow-unprotected-txs":true,
				"api-max-blocks-per-request":2000,
				"batch-request-limit":50,
				"api-max-duration":"30s",
				"state-sync-enabled":false,
				"warp-off-chain-messages":["0x1234"],
				"api-resolve-pending-to-last-executed":true,
				"state-sync-ids":["` + nodeID.String() + `"]
			}`,
			want: Config{
				PriceTarget:                  new(gas.Price(500)),
				GasTarget:                    new(gas.Gas(1500)),
				MinDelayTarget:               new(uint64(3000)),
				Pruning:                      false,
				CommitInterval:               256,
				TrieCleanCache:               256,
				SnapshotCache:                128,
				AllowMissingTries:            true,
				StateScheme:                  firewood.Scheme,
				LocalTxsEnabled:              true,
				TxPoolAccountSlots:           8,
				TxPoolGlobalSlots:            2048,
				APIs:                         set.Of(rpc.APIChain, rpc.APITrace),
				AllowUnprotectedTxs:          true,
				BatchRequestLimit:            50,
				APIMaxDuration:               Duration{Duration: 30 * time.Second},
				MaxBlocksPerRequest:          2000,
				ResolvePendingToLastExecuted: true,
				StateSyncEnabled:             false,
				WarpOffChainMessages:         []hexutil.Bytes{{0x12, 0x34}},
				internalConfig: internalConfig{
					StateSyncIDs: set.Of(nodeID),
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Logf("parsing config:\n%s", test.json)
			log := loggingtest.NewRecorder(logging.Warn)
			got := Default()
			err := Parse(log, []byte(test.json), &got)
			if diff := testerr.Diff(err, test.wantErr); diff != "" {
				t.Fatalf("Parse(...) error (-want +got)\n%s", diff)
			}
			if test.wantErr != nil {
				return
			}
			require.Equal(t, test.want, got, "Parse(...)")
			require.Equal(t, test.wantWarnings, log.Records, "Parse(...) logs")
		})
	}
}

// TestParse checks that keys of an embedded [Config] are recognized alongside
// the embedding VM's own keys, and that only other keys are warned about.
func TestParse(t *testing.T) {
	type vmConfig struct {
		Config

		Own uint64 `json:"own"`
	}

	log := loggingtest.NewRecorder(logging.Warn)
	got := vmConfig{Config: Default()}
	require.NoError(t, Parse(log, []byte(`{"commit-interval":7,"own":3,"unknown":true}`), &got))

	want := vmConfig{Config: Default(), Own: 3}
	want.CommitInterval = 7
	require.Equal(t, want, got)
	require.Equal(t, []*loggingtest.Record{{
		Level:  logging.Warn,
		Msg:    "ignoring unrecognized config options",
		Fields: []zap.Field{zap.Strings("options", []string{"unknown"})},
	}}, log.Records)
}

// TestSAEConfig checks that every field of [Config] that maps onto [sae.Config]
// sets the corresponding value.
func TestSAEConfig(t *testing.T) {
	c := Config{
		Pruning:                      false,
		CommitInterval:               7,
		LocalTxsEnabled:              true,
		TxPoolAccountSlots:           8,
		TxPoolGlobalSlots:            9,
		BatchRequestLimit:            10,
		APIMaxDuration:               Duration{11 * time.Second},
		ResolvePendingToLastExecuted: false,
	}
	got := c.SAEConfig(nil)

	require.False(t, got.MempoolConfig.NoLocals, "MempoolConfig.NoLocals")
	require.Equal(t, uint64(8), got.MempoolConfig.AccountSlots, "MempoolConfig.AccountSlots")
	require.Equal(t, uint64(9), got.MempoolConfig.GlobalSlots, "MempoolConfig.GlobalSlots")
	require.True(t, got.DBConfig.Archival, "DBConfig.Archival")
	require.Equal(t, uint64(7), got.DBConfig.CommitInterval, "DBConfig.CommitInterval")
	require.Equal(t, uint64(10), got.RPCConfig.BatchRequestLimit, "RPCConfig.BatchRequestLimit")
	require.Equal(t, 11*time.Second, got.RPCConfig.CallTimeout, "RPCConfig.CallTimeout")
	require.False(t, got.RPCConfig.ResolvePendingToLastExecuted, "RPCConfig.ResolvePendingToLastExecuted")
}

func TestVerify(t *testing.T) {
	tests := []struct {
		name      string
		config    Config
		networkID uint32
		wantErr   error
	}{
		{
			name:   "defaults",
			config: Default(),
		},
		{
			name:    "api/apis_unknown_name",
			config:  with(func(c *Config) { c.APIs = set.Of[rpc.API]("eth") }),
			wantErr: rpc.ErrUnknownAPI,
		},
		{
			name:    "api/batch_request_limit_too_large",
			config:  with(func(c *Config) { c.BatchRequestLimit = math.MaxInt64 + 1 }),
			wantErr: rpc.ErrBatchRequestLimitTooLarge,
		},
		{
			name:      "state/hash_commit_interval_custom_network",
			config:    with(func(c *Config) { c.CommitInterval = 256 }),
			networkID: constants.UnitTestID,
		},
		{
			name:      "state/hash_commit_interval_production_network",
			config:    with(func(c *Config) { c.CommitInterval = 256 }),
			networkID: constants.MainnetID,
			wantErr:   errProductionCommitInterval,
		},
		{
			name: "state/firewood_commit_interval_production_network",
			config: with(func(c *Config) {
				c.StateScheme = firewood.Scheme
				c.CommitInterval = 256
			}),
			networkID: constants.MainnetID,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Verify(tt.networkID)
			require.ErrorIs(t, err, tt.wantErr, "%T.Verify(%d)", tt.config, tt.networkID)
		})
	}
}

func TestStateSyncConfigCommitInterval(t *testing.T) {
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
			c := Default()
			c.StateScheme = firewood.Scheme
			c.CommitInterval = tt.commitInterval

			got := c.StateSyncConfig(tt.networkID).DBConfig.CommitInterval
			require.Equal(t, tt.want, got, "%T.StateSyncConfig(%d).DBConfig.CommitInterval", c, tt.networkID)
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
			c := Config{
				WarpOffChainMessages: test.bytes,
			}

			got, err := c.WarpMessages()
			require.ErrorIsf(t, err, test.wantErr, "%T.WarpMessages()", c)
			require.Equalf(t, test.want, got, "%T.WarpMessages()", c)
		})
	}
}
