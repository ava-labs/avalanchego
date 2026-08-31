// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
)

// vmHealthDetails mirrors the JSON that operators and tests consume from the
// node's health API. It is defined separately from [Health] so that a change to
// the reported field names fails this test.
type vmHealthDetails struct {
	State       string          `json:"state"`
	StateScheme string          `json:"stateScheme"`
	StateSync   json.RawMessage `json:"stateSync"`
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
				{state: snow.Initializing, wantState: HealthStateInitializing},
				{state: snow.StateSyncing, wantState: HealthStateStateSyncing},
				{state: snow.Bootstrapping, wantState: HealthStateBootstrapping},
				{state: snow.NormalOp, wantState: HealthStateNormalOp},
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
				// [VM] performs no state sync, so it MUST omit the detail
				// rather than report a misleading zero value.
				require.Emptyf(t, health.StateSync, "health state sync detail of %T", sut)
			}
		})
	}
}

// TestStateSyncProgressDetails asserts the mapping from an observed sync
// lifecycle to the reported [StateSync] detail, including that a summary is only
// reported once a sync starts and an error only once one fails.
func TestStateSyncProgressDetails(t *testing.T) {
	var (
		summaryHash = common.Hash(ids.GenerateTestID())
		syncErr     = errors.New("peers ran out of state")
	)

	// started is the lifecycle of a sync that was launched for summaryHash.
	started := StateSyncProgress{
		Enabled:       true,
		Started:       true,
		SummaryHeight: 4096,
		SummaryHash:   summaryHash,
	}

	tests := []struct {
		name     string
		progress StateSyncProgress
		want     StateSync
	}{
		{
			name:     "disabled",
			progress: StateSyncProgress{},
			want:     StateSync{Status: StateSyncDisabled},
		},
		{
			name:     "enabled_but_no_summary_offered",
			progress: StateSyncProgress{Enabled: true},
			want:     StateSync{Status: StateSyncNotStarted},
		},
		{
			name:     "summary_declined",
			progress: StateSyncProgress{Enabled: true, Skipped: true},
			want:     StateSync{Status: StateSyncSkipped},
		},
		{
			name:     "syncing",
			progress: started,
			want: StateSync{
				Status:        StateSyncSyncing,
				SummaryHeight: 4096,
				SummaryHash:   summaryHash.Hex(),
			},
		},
		{
			name: "completed",
			progress: func() StateSyncProgress {
				p := started
				p.Finished = true
				return p
			}(),
			want: StateSync{
				Status:        StateSyncCompleted,
				SummaryHeight: 4096,
				SummaryHash:   summaryHash.Hex(),
			},
		},
		{
			name: "failed",
			progress: func() StateSyncProgress {
				p := started
				p.Finished = true
				p.Err = syncErr
				return p
			}(),
			want: StateSync{
				Status:        StateSyncFailed,
				SummaryHeight: 4096,
				SummaryHash:   summaryHash.Hex(),
				Error:         syncErr.Error(),
			},
		},
		{
			// Err is only defined once the sync finishes, so an error observed
			// mid-sync MUST NOT be reported.
			name: "unfinished_error_is_not_reported",
			progress: func() StateSyncProgress {
				p := started
				p.Err = syncErr
				return p
			}(),
			want: StateSync{
				Status:        StateSyncSyncing,
				SummaryHeight: 4096,
				SummaryHash:   summaryHash.Hex(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.progress.Details()
			require.NotNilf(t, got, "%T.Details()", tt.progress)
			require.Equalf(t, tt.want, *got, "%T.Details()", tt.progress)
		})
	}
}
