// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmconfig

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/arr4n/shed/testerr"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/firewood"
	"github.com/ava-labs/avalanchego/vms/evm/sae/rpc"
)

func errIsType[T error]() testerr.Want {
	return testerr.As(func(T) string { return "" })
}

func TestParse(t *testing.T) {
	// with applies mod to Default() so each override case asserts that its
	// JSON field overrides the default while the rest are preserved.
	with := func(mod func(*Config)) Config {
		c := Default()
		mod(&c)
		return c
	}
	// parse parses b as the C-Chain and L1 VMs do.
	parse := func(log logging.Logger, b []byte) (Config, error) {
		c := Default()
		if err := Parse(log, b, &c); err != nil {
			return Config{}, err
		}
		if err := c.Verify(); err != nil {
			return Config{}, err
		}
		return c, nil
	}
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
			wantErr: errIsType[*json.SyntaxError](),
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
			name:    "api/apis_unknown_name",
			json:    `{"apis":["eth"]}`,
			wantErr: testerr.Is(rpc.ErrUnknownAPI),
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
			name:    "api/batch_request_limit_too_large",
			json:    `{"batch-request-limit":9223372036854775808}`, // math.MaxInt64 + 1
			wantErr: testerr.Is(rpc.ErrBatchRequestLimitTooLarge),
		},
		{
			name: "api/api_max_duration",
			json: `{"api-max-duration":"1m"}`,
			want: with(func(c *Config) { c.APIMaxDuration = Duration{time.Minute} }),
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
			want: with(func(c *Config) { c.ResolvePendingToLastExecuted = true }),
		},
		{
			name: "api/disable_map_pending_to_last_executed",
			json: `{"api-resolve-pending-to-last-executed":false}`,
			want: with(func(c *Config) { c.ResolvePendingToLastExecuted = false }),
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
				"state-sync-ids":["` + nodeID.String() + `"]
			}`,
			want: Config{
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
				APIMaxDuration:               Duration{30 * time.Second},
				ResolvePendingToLastExecuted: true,
				InternalConfig: InternalConfig{
					StateSyncIDs: set.Of(nodeID),
				},
			},
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
