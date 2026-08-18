// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"encoding/json"
	"testing"

	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
)

// vmHealthDetails mirrors the JSON that operators and tests consume from the
// node's health API. It is defined separately from [Health] so that a change to
// the reported field names fails this test.
type vmHealthDetails struct {
	State       string `json:"state"`
	StateScheme string `json:"stateScheme"`
}

// TestHealthCheck asserts that the VM reports its consensus state and trie
// database scheme as the health API serializes them.
func TestHealthCheck(t *testing.T) {
	tests := []struct {
		name            string
		scheme          string
		wantStateScheme string
	}{
		{
			name:            "default_scheme",
			scheme:          "",
			wantStateScheme: rawdb.HashScheme,
		},
		{
			name:            "hash_scheme",
			scheme:          rawdb.HashScheme,
			wantStateScheme: rawdb.HashScheme,
		},
		{
			name:            "firewood_scheme",
			scheme:          customrawdb.FirewoodScheme,
			wantStateScheme: customrawdb.FirewoodScheme,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, sut := newSUT(t, 0, options.Func[sutConfig](func(c *sutConfig) {
				c.vmConfig.DBConfig.Scheme = tt.scheme
			}))

			for _, test := range []struct {
				state     snow.State
				wantState string
			}{
				// [newSUT] leaves the VM in its initial state, so every
				// transition the engine drives can be observed.
				{state: snow.Initializing, wantState: healthStateInitializing},
				{state: snow.StateSyncing, wantState: healthStateStateSyncing},
				{state: snow.Bootstrapping, wantState: healthStateBootstrapping},
				{state: snow.NormalOp, wantState: healthStateNormalOp},
			} {
				if test.state != snow.Initializing {
					require.NoErrorf(t, sut.SetState(ctx, test.state), "%T.SetState(%s)", sut, test.state)
				}

				details, err := sut.HealthCheck(ctx)
				require.NoErrorf(t, err, "%T.HealthCheck()", sut)

				// Round-trip through JSON to assert on what the health API
				// actually serves.
				rawDetails, err := json.Marshal(details)
				require.NoErrorf(t, err, "marshaling %T.HealthCheck() details", sut)
				var health vmHealthDetails
				require.NoErrorf(t, json.Unmarshal(rawDetails, &health), "unmarshaling health details %s", rawDetails)

				require.Equalf(t, test.wantState, health.State, "health state after SetState(%s)", test.state)
				require.Equal(t, tt.wantStateScheme, health.StateScheme, "health state scheme")
			}
		})
	}
}
