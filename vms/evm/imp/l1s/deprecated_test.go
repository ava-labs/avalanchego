// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

// This file tests the deprecated Subnet-EVM options.
//
// TODO: delete this file together with deprecated.go.

import (
	"encoding/base64"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/logging"
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

	const (
		addr     = "0x0123456789abcdef0123456789abcdef01234567"
		dbConfig = `{"maxOpenFiles":1}`
	)
	dbConfigFile := filepath.Join(t.TempDir(), "db.json")
	require.NoError(t, os.WriteFile(dbConfigFile, []byte(dbConfig), 0o600))
	withDBConfig := with(func(c *config) { c.DatabaseConfig = json.RawMessage(dbConfig) })

	tests := []struct {
		name    string
		json    string
		want    config
		wantErr error
	}{
		{
			name: "fee_recipient",
			json: `{"feeRecipient":"0123456789abcdef0123456789abcdef01234567"}`,
			want: with(func(c *config) { c.FeeRecipient = common.HexToAddress(addr) }),
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
		},
		{
			name: "fee_recipient_superseded",
			json: `{"feeRecipient":"0xdead","fee-recipient":"` + addr + `"}`,
			want: with(func(c *config) { c.FeeRecipient = common.HexToAddress(addr) }),
		},
		{
			name: "database_config_base64",
			json: `{"database-config":"` + base64.StdEncoding.EncodeToString([]byte(dbConfig)) + `"}`,
			want: withDBConfig,
		},
		{
			name: "database_config_base64_empty",
			json: `{"database-config":""}`,
			want: defaultConfig(""),
		},
		{
			name:    "database_config_base64_invalid",
			json:    `{"database-config":"!"}`,
			wantErr: base64.CorruptInputError(0),
		},
		{
			name: "database_config_file",
			json: `{"database-config-file":` + string(mustMarshalJSON(t, dbConfigFile)) + `}`,
			want: withDBConfig,
		},
		{
			name:    "database_config_file_missing",
			json:    `{"database-config-file":"` + filepath.Join(t.TempDir(), "missing.json") + `"}`,
			wantErr: fs.ErrNotExist,
		},
		{
			name: "database_config_file_superseded",
			json: `{"database-config":` + dbConfig + `,"database-config-file":"missing.json"}`,
			want: withDBConfig,
		},
		{
			name: "eth_apis_subnet_evm_defaults",
			json: `{"eth-apis":["eth","eth-filter","net","web3","internal-eth","internal-blockchain","internal-transaction"]}`,
			want: with(func(c *config) {
				c.APIs = set.Of(
					validatorAPI,
					rpc.APIAvalanche,
					rpc.APIChain,
					rpc.APINet,
					rpc.APIPrice,
					rpc.APISubscription,
					rpc.APITx,
					rpc.APIWeb3,
				)
			}),
		},
		{
			name: "eth_apis_aliases",
			json: `{"eth-apis":["internal-public-tx-pool","private-debug"]}`,
			want: with(func(c *config) { c.APIs = set.Of(validatorAPI, rpc.APITxPool) }),
		},
		{
			name:    "eth_apis_unknown_name",
			json:    `{"eth-apis":["bogus"]}`,
			wantErr: errUnknownLegacyEthAPI,
		},
		{
			name: "validators_api_disabled",
			json: `{"validators-api-enabled":false}`,
			want: with(func(c *config) { c.APIs.Remove(validatorAPI) }),
		},
		{
			name: "warp_and_admin_apis_enabled",
			json: `{"eth-apis":["web3"],"warp-api-enabled":true,"admin-api-enabled":true}`,
			want: with(func(c *config) { c.APIs = set.Of(validatorAPI, rpc.APIWeb3) }),
		},
		{
			name: "api_options_superseded",
			json: `{"eth-apis":["bogus"],"validators-api-enabled":true,"apis":["net"]}`,
			want: with(func(c *config) { c.APIs = set.Of(rpc.APINet) }),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseConfig(&snow.Context{Log: logging.NoLog{}}, []byte(test.json))
			require.ErrorIs(t, err, test.wantErr)
			if test.wantErr != nil {
				return
			}
			require.Equal(t, test.want, got, "parseConfig(...)")
		})
	}
}
