// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/saevm/sae/rpc"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	require.Empty(t, config.MempoolConfig.Journal, "DefaultConfig().MempoolConfig.Journal")
	require.Equal(t, uint64(saedb.DefaultCommitInterval), config.DBConfig.CommitInterval, "DefaultConfig().DBConfig.CommitInterval")
	require.Equal(t, uint64(saedb.DefaultTrieCacheSizeMiB), config.DBConfig.TrieCacheMiB, "DefaultConfig().DBConfig.TrieCacheMiB")
	require.Equal(t, uint64(saedb.DefaultSnapshotCacheSizeMiB), config.DBConfig.SnapshotCacheMiB, "DefaultConfig().DBConfig.SnapshotCacheMiB")
	require.Equal(t, uint64(50_000_000), config.RPCConfig.GasCap, "DefaultConfig().RPCConfig.GasCap")
	require.InDelta(t, 100, config.RPCConfig.TxFeeCap, 0, "DefaultConfig().RPCConfig.TxFeeCap")
	require.Equal(t, uint64(1000), config.RPCConfig.BatchRequestLimit, "DefaultConfig().RPCConfig.BatchRequestLimit")
	require.True(t, config.RPCConfig.ResolvePendingToLastExecuted, "DefaultConfig().RPCConfig.ResolvePendingToLastExecuted")
}

func TestConfigVerify(t *testing.T) {
	valid := DefaultConfig()
	invalidDB := valid
	invalidDB.DBConfig.CommitInterval = 0
	invalidRPC := valid
	invalidRPC.RPCConfig.BatchRequestLimit = math.MaxUint64

	tests := []struct {
		name    string
		config  Config
		wantErr error
	}{
		{
			name:   "valid",
			config: valid,
		},
		{
			name:    "invalid_db",
			config:  invalidDB,
			wantErr: saedb.ErrZeroCommitInterval,
		},
		{
			name:    "invalid_rpc",
			config:  invalidRPC,
			wantErr: rpc.ErrBatchRequestLimitTooLarge,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.config.Verify()
			require.ErrorIs(t, err, test.wantErr, "Config.Verify()")
		})
	}
}
