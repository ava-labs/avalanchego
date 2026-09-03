// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

// This file tests the deprecated coreth (pre-SAE C-Chain) options.
//
// TODO(JonathanOppenheimer): delete this file together with deprecated.go!

import (
	"encoding/json"
	"testing"

	"github.com/arr4n/shed/testerr"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/saevm/sae/rpc"
)

func TestParseConfigDeprecated(t *testing.T) {
	// with applies mod to defaultConfig() so each case asserts that the
	// deprecated option overrides the default while the rest are preserved.
	with := func(mod func(*config)) config {
		c := defaultConfig()
		mod(&c)
		return c
	}

	tests := []struct {
		name         string
		json         string
		want         config
		wantWarnings []string
		wantErr      testerr.Want
	}{
		{
			name: "eth_apis_all_mapped_names",
			json: `{"eth-apis":["internal-eth","internal-blockchain","internal-transaction","internal-tx-pool","internal-debug","debug-tracer","debug-file-tracer","debug-handler","eth-filter","net","web3"]}`,
			want: with(func(c *config) {
				c.APIs = set.Of(
					rpc.APIAvalanche,
					rpc.APIChain,
					rpc.APIDB,
					rpc.APIGas,
					rpc.APINet,
					rpc.APIProfile,
					rpc.APISubscriptions,
					rpc.APITrace,
					rpc.APITransactions,
					rpc.APITxPool,
					rpc.APIWeb3,
				)
			}),
			wantWarnings: []string{`"eth-apis" is deprecated and will be removed in the next release; use "apis": ["avalanche","chain","db","gas","net","profile","subscriptions","trace","transactions","txpool","web3"] instead`},
		},
		{
			name: "eth_apis_coreth_defaults",
			json: `{"eth-apis":["eth","eth-filter","net","web3","internal-eth","internal-blockchain","internal-transaction"]}`,
			want: with(func(c *config) {
				c.APIs = set.Of(
					rpc.APIAvalanche,
					rpc.APIChain,
					rpc.APIGas,
					rpc.APINet,
					rpc.APISubscriptions,
					rpc.APITransactions,
					rpc.APIWeb3,
				)
			}),
			wantWarnings: []string{`"eth-apis" is deprecated and will be removed in the next release; use "apis": ["avalanche","chain","gas","net","subscriptions","transactions","web3"] instead; ignoring names whose methods no longer exist: ["eth"]`},
		},
		{
			name:         "eth_apis_only_removed_services",
			json:         `{"eth-apis":["admin","internal-personal"]}`,
			want:         with(func(c *config) { c.APIs.Clear() }),
			wantWarnings: []string{`"eth-apis" is deprecated and will be removed in the next release; use "apis": [] instead; ignoring names whose methods no longer exist: ["admin","internal-personal"]`},
		},
		{
			name: "eth_apis_combined_with_unrecognized_option",
			json: `{"eth-apis":["web3"],"rpc-gas-cap":50}`,
			want: with(func(c *config) { c.APIs = set.Of(rpc.APIWeb3) }),
			wantWarnings: []string{
				`"eth-apis" is deprecated and will be removed in the next release; use "apis": ["web3"] instead`,
				`ignoring unrecognized options: ["rpc-gas-cap"]`,
			},
		},
		{
			name:         "eth_apis_superseded_by_apis",
			json:         `{"eth-apis":["bogus"],"apis":["net","web3"]}`,
			want:         with(func(c *config) { c.APIs = set.Of(rpc.APINet, rpc.APIWeb3) }),
			wantWarnings: []string{`ignoring deprecated "eth-apis" because "apis" is set; "eth-apis" will be removed in the next release`},
		},
		{
			name:    "eth_apis_unknown_name",
			json:    `{"eth-apis":["bogus"]}`,
			wantErr: testerr.Is(errUnknownLegacyEthAPI),
		},
		{
			name:    "eth_apis_not_an_array",
			json:    `{"eth-apis":5}`,
			wantErr: errIsType[*json.UnmarshalTypeError](),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Logf("parsing config:\n%s", test.json)
			got, gotWarnings, err := parseConfig([]byte(test.json), 0)
			if diff := testerr.Diff(err, test.wantErr); diff != "" {
				t.Errorf("parseConfig(...) error (-want +got)\n%s", diff)
			}
			require.Equal(t, test.want, got, "parseConfig(...)")
			require.Equal(t, test.wantWarnings, gotWarnings, "parseConfig(...) warnings")
		})
	}
}
