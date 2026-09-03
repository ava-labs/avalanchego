// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/arr4n/shed/testerr"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
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
	nodeID := ids.GenerateTestNodeID()

	tests := []struct {
		name         string
		json         string
		networkID    uint32
		want         config
		wantWarnings []*loggingtest.Record
		wantErr      testerr.Want
	}{
		// Defaults and errors
		{
			name:    "defaults/invalid_json",
			json:    "invalid",
			wantErr: errIsType[*json.SyntaxError](),
		},
		{
			name: "defaults/empty_input",
			want: defaultConfig(),
		},
		{
			name: "defaults/empty_object",
			json: `{}`,
			want: defaultConfig(),
		},

		// Block building
		{
			name: "block_building/min_price_target",
			json: `{"min-price-target":1000}`,
			want: with(func(c *config) { c.PriceTarget = utils.PointerTo(gas.Price(1000)) }),
		},
		{
			// An explicit 0 is a vote for the minimum, distinct from an absent
			// field, which is no vote.
			name: "block_building/min_price_target_explicit_zero",
			json: `{"min-price-target":0}`,
			want: with(func(c *config) { c.PriceTarget = utils.PointerTo(gas.Price(0)) }),
		},
		{
			name: "block_building/gas_target",
			json: `{"gas-target":1000}`,
			want: with(func(c *config) { c.GasTarget = utils.PointerTo(gas.Gas(1000)) }),
		},
		{
			name: "block_building/min_delay_target",
			json: `{"min-delay-target":2000}`,
			want: with(func(c *config) { c.MinDelayTarget = utils.PointerTo[uint64](2000) }),
		},

		// State & trie
		{
			name: "state/pruning_disabled",
			json: `{"pruning-enabled":false}`,
			want: with(func(c *config) { c.Pruning = false }),
		},
		{
			name: "state_scheme",
			json: `{"state-scheme":"firewood"}`,
			want: with(func(c *config) { c.StateScheme = customrawdb.FirewoodScheme }),
		},
		{
			name:      "state/commit_interval",
			json:      `{"commit-interval":256}`,
			networkID: constants.UnitTestID,
			want:      with(func(c *config) { c.CommitInterval = 256 }),
		},
		{
			name:      "state/commit_interval_production_network",
			json:      `{"commit-interval":256}`,
			networkID: constants.MainnetID,
			wantErr:   testerr.Is(errProductionCommitInterval),
		},
		{
			name: "state/trie_clean_cache",
			json: `{"trie-clean-cache":256}`,
			want: with(func(c *config) { c.TrieCleanCache = 256 }),
		},
		{
			name: "state/snapshot_cache",
			json: `{"snapshot-cache":128}`,
			want: with(func(c *config) { c.SnapshotCache = 128 }),
		},
		{
			name: "state/allow_missing_tries",
			json: `{"allow-missing-tries":true}`,
			want: with(func(c *config) { c.AllowMissingTries = true }),
		},

		// Transaction pool
		{
			name: "tx_pool/local_txs_enabled",
			json: `{"local-txs-enabled":true}`,
			want: with(func(c *config) { c.LocalTxsEnabled = true }),
		},
		{
			name: "tx_pool/slots",
			json: `{"tx-pool-account-slots":32,"tx-pool-global-slots":9000}`,
			want: with(func(c *config) {
				c.TxPoolAccountSlots = 32
				c.TxPoolGlobalSlots = 9000
			}),
		},

		// APIs
		{
			name: "api/apis",
			json: `{"apis":["subscriptions","db","net","web3"]}`,
			want: with(func(c *config) {
				c.APIs = set.Of(rpc.APISubscriptions, rpc.APIDB, rpc.APINet, rpc.APIWeb3)
			}),
		},
		{
			name: "api/apis_explicit_empty",
			json: `{"apis":[]}`,
			want: with(func(c *config) { c.APIs.Clear() }),
		},
		{
			name:    "api/apis_unknown_name",
			json:    `{"apis":["eth"]}`,
			wantErr: testerr.Is(rpc.ErrUnknownAPI),
		},
		{
			name: "api/allow_unprotected_txs",
			json: `{"allow-unprotected-txs":true}`,
			want: with(func(c *config) { c.AllowUnprotectedTxs = true }),
		},
		{
			name: "api/batch_request_limit",
			json: `{"batch-request-limit":50}`,
			want: with(func(c *config) { c.BatchRequestLimit = 50 }),
		},
		{
			name: "api/batch_request_limit_explicit_zero",
			json: `{"batch-request-limit":0}`, // 0 disables the batch limit
			want: with(func(c *config) { c.BatchRequestLimit = 0 }),
		},
		{
			name:    "api/batch_request_limit_too_large",
			json:    `{"batch-request-limit":9223372036854775808}`, // math.MaxInt64 + 1
			wantErr: testerr.Is(rpc.ErrBatchRequestLimitTooLarge),
		},
		{
			name: "api/api_max_duration",
			json: `{"api-max-duration":"1m"}`,
			want: with(func(c *config) { c.APIMaxDuration = duration{time.Minute} }),
		},
		{
			name:    "api/api_max_duration_number",
			json:    `{"api-max-duration":5000000000}`,
			wantErr: errIsType[*json.UnmarshalTypeError](),
		},
		{
			name:    "api/api_max_duration_unparseable",
			json:    `{"api-max-duration":"not-a-duration"}`,
			wantErr: testerr.Contains("invalid duration"),
		},
		{
			name: "api/enable_map_pending_to_last_executed",
			json: `{"api-resolve-pending-to-last-executed":true}`,
			want: with(func(c *config) { c.ResolvePendingToLastExecuted = true }),
		},
		{
			name: "api/disable_map_pending_to_last_executed",
			json: `{"api-resolve-pending-to-last-executed":false}`,
			want: with(func(c *config) { c.ResolvePendingToLastExecuted = false }),
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

		// Internal
		{
			name: "internal/state_sync_ids",
			json: fmt.Sprintf(`{"state-sync-ids":["%s"]}`, nodeID),
			want: with(func(c *config) {
				c.StateSyncIDs = set.Of(nodeID)
			}),
		},

		// Unrecognized options
		{
			name: "unrecognized/options",
			json: `{"rpc-gas-cap":50,"hello austin larson":1,"trie-clean-cache":256}`,
			want: with(func(c *config) { c.TrieCleanCache = 256 }),
			wantWarnings: []*loggingtest.Record{{
				Level:  logging.Warn,
				Msg:    "ignoring unrecognized C-Chain config options",
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
				"batch-request-limit":50,
				"api-max-duration":"30s",
				"state-sync-enabled":false,
				"warp-off-chain-messages":["0x1234"],
				"api-resolve-pending-to-last-executed":true,
				"state-sync-ids":["` + nodeID.String() + `"]
			}`,
			want: config{
				PriceTarget:                  utils.PointerTo(gas.Price(500)),
				GasTarget:                    utils.PointerTo(gas.Gas(1500)),
				MinDelayTarget:               utils.PointerTo[uint64](3000),
				Pruning:                      false,
				StateScheme:                  customrawdb.FirewoodScheme,
				CommitInterval:               256,
				TrieCleanCache:               256,
				SnapshotCache:                128,
				AllowMissingTries:            true,
				LocalTxsEnabled:              true,
				TxPoolAccountSlots:           8,
				TxPoolGlobalSlots:            2048,
				APIs:                         set.Of(rpc.APIChain, rpc.APITrace),
				AllowUnprotectedTxs:          true,
				BatchRequestLimit:            50,
				APIMaxDuration:               duration{30 * time.Second},
				WarpOffChainMessages:         []hexutil.Bytes{{0x12, 0x34}},
				ResolvePendingToLastExecuted: true,
				StateSyncEnabled:             false,
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

			got, err := sut.ethclient.PendingCodeAt(ctx, addr)
			if diff := testerr.Diff(err, tt.wantErr); diff != "" {
				t.Fatalf("%T.PendingCodeAt(%v): %s", sut.ethclient, addr, diff)
			}
			if tt.wantErr != nil {
				return
			}
			assert.Equalf(t, code, got, "%T.PendingCodeAt(%v)", sut.ethclient, addr)
		})
	}
}
