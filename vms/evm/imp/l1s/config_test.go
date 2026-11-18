// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/arr4n/shed/testerr"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/firewood"
	"github.com/ava-labs/avalanchego/vms/evm/imp/vmconfig"
	"github.com/ava-labs/avalanchego/vms/evm/sae/rpc"
)

// TestParseConfig covers the L1-specific parsing. The options shared with the
// C-Chain are covered by the C-Chain's TestParseConfig.
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
		{
			name:      "state/hash_commit_interval_production_network",
			json:      `{"commit-interval":256}`,
			networkID: constants.MainnetID,
			wantErr:   testerr.Is(vmconfig.ErrProductionCommitInterval),
		},

		// Unrecognized options
		{
			name: "unrecognized/not_yet_supported",
			json: `{"min-delay-target":2000,"state-sync-enabled":false,"warp-off-chain-messages":[],"feeRecipient":"0x01","use-standalone-database":true,"trie-clean-cache":256}`,
			want: with(func(c *config) { c.TrieCleanCache = 256 }),
			wantWarnings: []*loggingtest.Record{{
				Level:  logging.Warn,
				Msg:    "ignoring unrecognized config options",
				Fields: []zap.Field{zap.Strings("options", []string{"feeRecipient", "min-delay-target", "state-sync-enabled", "use-standalone-database", "warp-off-chain-messages"})},
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
			want: config{
				Config: vmconfig.Config{
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
					APIMaxDuration:               vmconfig.Duration{Duration: 30 * time.Second},
					ResolvePendingToLastExecuted: true,
					InternalConfig: vmconfig.InternalConfig{
						StateSyncIDs: set.Of(nodeID),
					},
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
				options.Func[sutConfig](func(c *sutConfig) {
					c.genesis.Alloc[addr] = types.Account{Code: code, Balance: new(big.Int)}
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
