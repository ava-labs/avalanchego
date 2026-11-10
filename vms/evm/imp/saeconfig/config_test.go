// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saeconfig

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/sae"
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

// TestApply checks that every field of [Config] that maps onto [sae.Config]
// overwrites the corresponding value.
func TestApply(t *testing.T) {
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
	got := sae.DefaultConfig()
	c.Apply(&got)

	require.False(t, got.MempoolConfig.NoLocals, "MempoolConfig.NoLocals")
	require.Equal(t, uint64(8), got.MempoolConfig.AccountSlots, "MempoolConfig.AccountSlots")
	require.Equal(t, uint64(9), got.MempoolConfig.GlobalSlots, "MempoolConfig.GlobalSlots")
	require.True(t, got.DBConfig.Archival, "DBConfig.Archival")
	require.Equal(t, uint64(7), got.DBConfig.CommitInterval, "DBConfig.CommitInterval")
	require.Equal(t, uint64(10), got.RPCConfig.BatchRequestLimit, "RPCConfig.BatchRequestLimit")
	require.Equal(t, 11*time.Second, got.RPCConfig.CallTimeout, "RPCConfig.CallTimeout")
	require.False(t, got.RPCConfig.ResolvePendingToLastExecuted, "RPCConfig.ResolvePendingToLastExecuted")
}
