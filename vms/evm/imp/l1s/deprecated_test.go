// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

// This file tests the deprecated Subnet-EVM options.
//
// TODO: delete this file together with deprecated.go.

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/sae/rpc"
)

// TestParseConfigDeprecated asserts that each deprecated option MUST convert
// to its replacement, which MUST take precedence when also set.
func TestParseConfigDeprecated(t *testing.T) {
	// with applies mod to defaultConfig so each case asserts that the
	// deprecated option overrides the default while the rest are preserved.
	with := func(mod func(*config)) config {
		c := defaultConfig("")
		mod(&c)
		return c
	}

	deprecatedAPIs := func(apis ...string) *loggingtest.Record {
		if apis == nil {
			apis = []string{} // zap.Strings distinguishes nil from empty
		}
		return &loggingtest.Record{
			Level:  logging.Warn,
			Msg:    `deprecated API options will be removed in the next release; set "apis" instead`,
			Fields: []zap.Field{zap.Strings("apis", apis)},
		}
	}
	removed := func(options ...string) *loggingtest.Record {
		return &loggingtest.Record{
			Level:  logging.Warn,
			Msg:    `ignoring deprecated API options whose methods no longer exist`,
			Fields: []zap.Field{zap.Strings("options", options)},
		}
	}

	const addr = "0x0123456789abcdef0123456789abcdef01234567"
	tests := []struct {
		name         string
		json         string
		want         config
		wantWarnings []*loggingtest.Record
		wantErr      error
	}{
		{
			name: "fee_recipient",
			json: `{"feeRecipient":"0123456789abcdef0123456789abcdef01234567"}`,
			want: with(func(c *config) { c.FeeRecipient = common.HexToAddress(addr) }),
			wantWarnings: []*loggingtest.Record{{
				Level:  logging.Warn,
				Msg:    `"feeRecipient" is deprecated and will be removed in the next release; set "fee-recipient" instead`,
				Fields: []zap.Field{zap.Stringer("fee-recipient", common.HexToAddress(addr))},
			}},
		},
		{
			name: "fee_recipient_empty",
			json: `{"feeRecipient":""}`,
			want: defaultConfig(""),
		},
		{
			name: "fee_recipient_invalid",
			json: `{"feeRecipient":"0xdead"}`,
			want: defaultConfig(""),
			wantWarnings: []*loggingtest.Record{{
				Level:  logging.Warn,
				Msg:    `ignoring deprecated "feeRecipient" because it isn't a hex address; fees will be burned`,
				Fields: []zap.Field{zap.String("feeRecipient", "0xdead")},
			}},
		},
		{
			name: "fee_recipient_superseded",
			json: `{"feeRecipient":"0xdead","fee-recipient":"` + addr + `"}`,
			want: with(func(c *config) { c.FeeRecipient = common.HexToAddress(addr) }),
			wantWarnings: []*loggingtest.Record{{
				Level: logging.Warn,
				Msg:   `ignoring deprecated "feeRecipient" because "fee-recipient" is set; "feeRecipient" will be removed in the next release`,
			}},
		},
		{
			name: "eth_apis_subnet_evm_defaults",
			json: `{"eth-apis":["eth","eth-filter","net","web3","internal-eth","internal-blockchain","internal-transaction"]}`,
			want: with(func(c *config) {
				c.APIs = set.Of(
					apiValidator,
					rpc.APIAvalanche,
					rpc.APIChain,
					rpc.APINet,
					rpc.APIPrice,
					rpc.APISubscription,
					rpc.APITx,
					rpc.APIWeb3,
				)
			}),
			wantWarnings: []*loggingtest.Record{
				deprecatedAPIs("avalanche", "chain", "net", "price", "subscription", "tx", "validator", "web3"),
				removed("eth"),
			},
		},
		{
			name: "eth_apis_aliases",
			json: `{"eth-apis":["internal-public-tx-pool","private-debug"]}`,
			want: with(func(c *config) { c.APIs = set.Of(apiValidator, rpc.APITxPool) }),
			wantWarnings: []*loggingtest.Record{
				deprecatedAPIs("txpool", "validator"),
				removed("private-debug"),
			},
		},
		{
			name:    "eth_apis_unknown_name",
			json:    `{"eth-apis":["bogus"]}`,
			wantErr: errUnknownLegacyEthAPI,
		},
		{
			name: "validators_api_disabled",
			json: `{"validators-api-enabled":false}`,
			want: with(func(c *config) { c.APIs.Remove(apiValidator) }),
			wantWarnings: []*loggingtest.Record{
				deprecatedAPIs("avalanche", "chain", "filter", "logs", "net", "price", "subscription", "trace", "tx", "txpool", "web3"),
			},
		},
		{
			name: "warp_and_admin_apis_enabled",
			json: `{"eth-apis":["web3"],"warp-api-enabled":true,"admin-api-enabled":true}`,
			want: with(func(c *config) { c.APIs = set.Of(apiValidator, rpc.APIWeb3) }),
			wantWarnings: []*loggingtest.Record{
				deprecatedAPIs("validator", "web3"),
				removed("warp-api-enabled", "admin-api-enabled"),
			},
		},
		{
			name: "api_options_superseded",
			json: `{"eth-apis":["bogus"],"validators-api-enabled":true,"apis":["net"]}`,
			want: with(func(c *config) { c.APIs = set.Of(rpc.APINet) }),
			wantWarnings: []*loggingtest.Record{{
				Level: logging.Warn,
				Msg:   `ignoring deprecated API options because "apis" is set; they will be removed in the next release`,
			}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log := loggingtest.NewRecorder(logging.Warn)
			got, err := parseConfig(&snow.Context{Log: log}, []byte(test.json))
			require.ErrorIs(t, err, test.wantErr)
			if test.wantErr != nil {
				return
			}
			require.Equal(t, test.want, got, "parseConfig(...)")
			require.Equal(t, test.wantWarnings, log.Records, "parseConfig(...) logs")
		})
	}
}
