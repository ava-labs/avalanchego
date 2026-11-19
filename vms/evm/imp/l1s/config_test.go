// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/arr4n/shed/testerr"
	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/sae/rpc"
)

// TestParseConfig asserts that options specific to the L1 VM MUST override
// [defaultConfig] and pass verification.
func TestParseConfig(t *testing.T) {
	snowCtx := snowtest.Context(t, ids.GenerateTestID())
	defaults := defaultConfig(snowCtx.ChainDataDir)

	with := func(mod func(*config)) config {
		c := defaultConfig(snowCtx.ChainDataDir)
		mod(&c)
		return c
	}

	const addr = "0x0123456789abcdef0123456789abcdef01234567"
	dbPath := filepath.Join(t.TempDir(), "db")

	tests := []struct {
		name    string
		json    string
		want    config
		wantErr testerr.Want
	}{
		{
			name: "empty_input",
			want: defaults,
		},
		{
			name: "empty_object",
			json: `{}`,
			want: defaults,
		},
		{
			name: "fee_recipient",
			json: `{"fee-recipient":"` + addr + `"}`,
			want: with(func(c *config) {
				c.FeeRecipient = common.HexToAddress(addr)
			}),
		},
		{
			name:    "fee_recipient_missing_prefix",
			json:    `{"fee-recipient":"0123456789abcdef0123456789abcdef01234567"}`,
			wantErr: errIsType[*json.UnmarshalTypeError](),
		},
		{
			name: "standalone_database",
			json: `{
				"use-standalone-database":false,
				"database-config":{"maxOpenFiles":1},
				"database-path":` + string(marshalJSON(dbPath)) + `,
				"database-type":"leveldb"
			}`,
			want: with(func(c *config) {
				c.UseStandaloneDatabase = false
				c.DatabaseConfig = json.RawMessage(`{"maxOpenFiles":1}`)
				c.DatabasePath = dbPath
				c.DatabaseType = leveldb.Name
			}),
		},
		{
			name: "apis_with_validator",
			json: `{"apis":["validator","net"]}`,
			want: with(func(c *config) {
				c.APIs = set.Of(validatorAPI, rpc.APINet)
			}),
		},
		{
			name: "apis_without_validator",
			json: `{"apis":["net"]}`,
			want: with(func(c *config) {
				c.APIs = set.Of(rpc.APINet)
			}),
		},
		{
			name:    "apis_unknown_name",
			json:    `{"apis":["bogus"]}`,
			wantErr: testerr.Is(rpc.ErrUnknownAPI),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseConfig(snowCtx, []byte(test.json))
			if diff := testerr.Diff(err, test.wantErr); diff != "" {
				t.Fatalf("parseConfig(...) error (-want +got)\n%s", diff)
			}
			require.Equal(t, test.want, got, "parseConfig(...)")
		})
	}
}
