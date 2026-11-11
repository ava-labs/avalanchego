// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saeconfig

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/firewood"
	"github.com/ava-labs/avalanchego/vms/evm/saedb"
)

// TestParse checks that keys of an embedded [Config] are recognized alongside
// the embedding VM's own keys, and that only other keys are warned about.
func TestParse(t *testing.T) {
	type vmConfig struct {
		Config

		Own uint64 `json:"own"`
	}

	log := loggingtest.NewRecorder(logging.Warn)
	got := vmConfig{Config: Default()}
	require.NoError(t, Parse(log, []byte(`{"commit-interval":7,"own":3,"unknown":true}`), &got))

	want := vmConfig{Config: Default(), Own: 3}
	want.CommitInterval = 7
	require.Equal(t, want, got)
	require.Equal(t, []*loggingtest.Record{{
		Level:  logging.Warn,
		Msg:    "ignoring unrecognized config options",
		Fields: []zap.Field{zap.Strings("options", []string{"unknown"})},
	}}, log.Records)
}

func TestParseEmptyLeavesConfigUnchanged(t *testing.T) {
	got := Default()
	require.NoError(t, Parse(logging.NoLog{}, nil, &got))
	require.Equal(t, Default(), got)
}

func TestDurationRejectsNumber(t *testing.T) {
	var d Duration
	var typeErr *json.UnmarshalTypeError
	require.ErrorAs(t, json.Unmarshal([]byte(`5000000000`), &d), &typeErr)
}

// TestSAEConfig checks that every field of [Config] that maps onto [sae.Config]
// sets the corresponding value.
func TestSAEConfig(t *testing.T) {
	c := Config{
		Pruning:                      false,
		CommitInterval:               7,
		LocalTxsEnabled:              true,
		TxPoolAccountSlots:           8,
		TxPoolGlobalSlots:            9,
		BatchRequestLimit:            10,
		APIMaxDuration:               Duration{11 * time.Second},
		ResolvePendingToLastExecuted: false,
	}
	got := c.SAEConfig(nil)

	require.False(t, got.MempoolConfig.NoLocals, "MempoolConfig.NoLocals")
	require.Equal(t, uint64(8), got.MempoolConfig.AccountSlots, "MempoolConfig.AccountSlots")
	require.Equal(t, uint64(9), got.MempoolConfig.GlobalSlots, "MempoolConfig.GlobalSlots")
	require.True(t, got.DBConfig.Archival, "DBConfig.Archival")
	require.Equal(t, uint64(7), got.DBConfig.CommitInterval, "DBConfig.CommitInterval")
	require.Equal(t, uint64(10), got.RPCConfig.BatchRequestLimit, "RPCConfig.BatchRequestLimit")
	require.Equal(t, 11*time.Second, got.RPCConfig.CallTimeout, "RPCConfig.CallTimeout")
	require.False(t, got.RPCConfig.ResolvePendingToLastExecuted, "RPCConfig.ResolvePendingToLastExecuted")
}

func TestVerifyProductionCommitInterval(t *testing.T) {
	tests := []struct {
		name        string
		networkID   uint32
		stateScheme string
		wantErr     error
	}{
		{
			name:      "custom_network",
			networkID: constants.UnitTestID,
		},
		{
			name:      "mainnet_hash",
			networkID: constants.MainnetID,
			wantErr:   errProductionCommitInterval,
		},
		{
			name:        "mainnet_firewood",
			networkID:   constants.MainnetID,
			stateScheme: firewood.Scheme,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Default()
			c.StateScheme = tt.stateScheme
			c.CommitInterval = 256

			err := c.Verify(tt.networkID)
			require.ErrorIs(t, err, tt.wantErr, "%T.Verify(%d)", c, tt.networkID)
		})
	}
}

func TestStateSyncConfigCommitInterval(t *testing.T) {
	tests := []struct {
		name           string
		networkID      uint32
		commitInterval uint64
		want           uint64
	}{
		{
			name:           "custom_network",
			networkID:      constants.UnitTestID,
			commitInterval: 256,
			want:           256,
		},
		{
			name:           "mainnet",
			networkID:      constants.MainnetID,
			commitInterval: 256,
			want:           saedb.DefaultCommitInterval,
		},
		{
			name:           "fuji",
			networkID:      constants.FujiID,
			commitInterval: 256,
			want:           saedb.DefaultCommitInterval,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Default()
			c.StateScheme = firewood.Scheme
			c.CommitInterval = tt.commitInterval

			got := c.StateSyncConfig(tt.networkID).DBConfig.CommitInterval
			require.Equal(t, tt.want, got, "%T.StateSyncConfig(%d).DBConfig.CommitInterval", c, tt.networkID)
		})
	}
}
