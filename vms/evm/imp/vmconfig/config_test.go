// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmconfig

import (
	"encoding/json"
	"fmt"
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
	"github.com/ava-labs/avalanchego/vms/evm/firewood"
	"github.com/ava-labs/avalanchego/vms/evm/sae/rpc"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

func errIsType[T error]() testerr.Want {
	return testerr.As(func(T) string { return "" })
}

// testConfig embeds [Config] alongside a field of its own, as a VM's config
// does.
type testConfig struct {
	Config
	Extra string `json:"extra"`
}

func TestParse(t *testing.T) {
	// with applies mod to Default() so each override case asserts that its
	// JSON field overrides the default while the rest are preserved.
	with := func(mod func(*testConfig)) testConfig {
		c := testConfig{Config: Default()}
		mod(&c)
		return c
	}
	parse := func(log logging.Logger, b []byte) (testConfig, error) {
		c := testConfig{Config: Default()}
		if _, err := Parse(log, b, &c); err != nil {
			return testConfig{}, err
		}
		if err := c.Verify(); err != nil {
			return testConfig{}, err
		}
		return c, nil
	}
	nodeID := ids.GenerateTestNodeID()

	tests := []struct {
		name         string
		json         string
		want         testConfig
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
			want: with(func(*testConfig) {}),
		},
		{
			name: "defaults/empty_object",
			json: `{}`,
			want: with(func(*testConfig) {}),
		},

		// Block building
		{
			name: "block_building/min_delay_target",
			json: `{"min-delay-target":2000}`,
			want: with(func(c *testConfig) { c.MinDelayTarget = new(uint64(2000)) }),
		},

		// State & trie
		{
			name: "state/pruning_disabled",
			json: `{"pruning-enabled":false}`,
			want: with(func(c *testConfig) { c.Pruning = false }),
		},
		{
			name: "state_scheme",
			json: `{"state-scheme":"firewood"}`,
			want: with(func(c *testConfig) { c.StateScheme = firewood.Scheme }),
		},
		{
			name: "state/commit_interval",
			json: `{"commit-interval":256}`,
			want: with(func(c *testConfig) { c.CommitInterval = 256 }),
		},
		{
			name: "state/trie_clean_cache",
			json: `{"trie-clean-cache":256}`,
			want: with(func(c *testConfig) { c.TrieCleanCache = 256 }),
		},
		{
			name: "state/snapshot_cache",
			json: `{"snapshot-cache":128}`,
			want: with(func(c *testConfig) { c.SnapshotCache = 128 }),
		},
		{
			name: "state/allow_missing_tries",
			json: `{"allow-missing-tries":true}`,
			want: with(func(c *testConfig) { c.AllowMissingTries = true }),
		},

		// Transaction pool
		{
			name: "tx_pool/local_txs_enabled",
			json: `{"local-txs-enabled":true}`,
			want: with(func(c *testConfig) { c.LocalTxsEnabled = true }),
		},
		{
			name: "tx_pool/slots",
			json: `{"tx-pool-account-slots":32,"tx-pool-global-slots":9000}`,
			want: with(func(c *testConfig) {
				c.TxPoolAccountSlots = 32
				c.TxPoolGlobalSlots = 9000
			}),
		},

		// APIs
		{
			name: "api/apis",
			json: `{"apis":["subscription","db","net","web3"]}`,
			want: with(func(c *testConfig) {
				c.APIs = set.Of(rpc.APISubscription, rpc.APIDB, rpc.APINet, rpc.APIWeb3)
			}),
		},
		{
			name: "api/apis_explicit_empty",
			json: `{"apis":[]}`,
			want: with(func(c *testConfig) { c.APIs.Clear() }),
		},
		{
			name:    "api/apis_unknown_name",
			json:    `{"apis":["eth"]}`,
			wantErr: testerr.Is(rpc.ErrUnknownAPI),
		},
		{
			name: "api/allow_unprotected_txs",
			json: `{"allow-unprotected-txs":true}`,
			want: with(func(c *testConfig) { c.AllowUnprotectedTxs = true }),
		},
		{
			name: "api/max_blocks_per_request",
			json: `{"api-max-blocks-per-request":2000}`,
			want: with(func(c *testConfig) { c.MaxBlocksPerRequest = 2000 }),
		},
		{
			name: "api/batch_request_limit",
			json: `{"batch-request-limit":50}`,
			want: with(func(c *testConfig) { c.BatchRequestLimit = 50 }),
		},
		{
			name: "api/batch_request_limit_explicit_zero",
			json: `{"batch-request-limit":0}`, // 0 disables the batch limit
			want: with(func(c *testConfig) { c.BatchRequestLimit = 0 }),
		},
		{
			name:    "api/batch_request_limit_too_large",
			json:    `{"batch-request-limit":9223372036854775808}`, // math.MaxInt64 + 1
			wantErr: testerr.Is(rpc.ErrBatchRequestLimitTooLarge),
		},
		{
			name: "api/api_max_duration",
			json: `{"api-max-duration":"1m"}`,
			want: with(func(c *testConfig) { c.APIMaxDuration = duration{time.Minute} }),
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
			want: with(func(c *testConfig) { c.ResolvePendingToLastExecuted = true }),
		},
		{
			name: "api/disable_map_pending_to_last_executed",
			json: `{"api-resolve-pending-to-last-executed":false}`,
			want: with(func(c *testConfig) { c.ResolvePendingToLastExecuted = false }),
		},

		// State sync
		{
			name: "state_sync/enabled",
			json: `{"state-sync-enabled":true}`,
			want: with(func(c *testConfig) { c.StateSyncEnabled = true }),
		},

		// Warp
		{
			name: "warp/off_chain_messages",
			json: `{"warp-off-chain-messages":["0x1234"]}`,
			want: with(func(c *testConfig) {
				c.WarpOffChainMessages = []hexutil.Bytes{{0x12, 0x34}}
			}),
		},

		// Embedding config
		{
			name: "embedding/extra",
			json: `{"extra":"hello from the past"}`,
			want: with(func(c *testConfig) { c.Extra = "hello from the past" }),
		},

		// Internal
		{
			name: "internal/state_sync_ids",
			json: fmt.Sprintf(`{"state-sync-ids":["%s"]}`, nodeID),
			want: with(func(c *testConfig) {
				c.StateSyncIDs = set.Of(nodeID)
			}),
		},

		// Unrecognized options
		{
			name: "unrecognized/options",
			json: `{"rpc-gas-cap":50,"hello austin larson":1,"trie-clean-cache":256,"extra":"hello from the past"}`,
			want: with(func(c *testConfig) {
				c.TrieCleanCache = 256
				c.Extra = "hello from the past"
			}),
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
				"api-resolve-pending-to-last-executed":true,
				"state-sync-enabled":true,
				"warp-off-chain-messages":["0x1234"],
				"state-sync-ids":["` + nodeID.String() + `"],
				"extra":"hello from the past"
			}`,
			want: testConfig{Config: Config{
				MinDelayTarget:               new(uint64(3000)),
				Pruning:                      false,
				StateScheme:                  firewood.Scheme,
				CommitInterval:               256,
				TrieCleanCache:               256,
				SnapshotCache:                128,
				AllowMissingTries:            true,
				LocalTxsEnabled:              true,
				TxPoolAccountSlots:           8,
				TxPoolGlobalSlots:            2048,
				APIs:                         set.Of(rpc.APIChain, rpc.APITrace),
				AllowUnprotectedTxs:          true,
				MaxBlocksPerRequest:          2000,
				BatchRequestLimit:            50,
				APIMaxDuration:               duration{30 * time.Second},
				ResolvePendingToLastExecuted: true,
				StateSyncEnabled:             true,
				WarpOffChainMessages:         []hexutil.Bytes{{0x12, 0x34}},
				internalConfig: internalConfig{
					StateSyncIDs: set.Of(nodeID),
				},
			}, Extra: "hello from the past"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Logf("parsing config:\n%s", test.json)
			log := loggingtest.NewRecorder(logging.Warn)
			got, err := parse(log, []byte(test.json))
			if diff := testerr.Diff(err, test.wantErr); diff != "" {
				t.Errorf("Parse(...) error (-want +got)\n%s", diff)
			}
			require.Equal(t, test.want, got, "Parse(...)")
			require.Equal(t, test.wantWarnings, log.Records, "Parse(...) logs")
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
