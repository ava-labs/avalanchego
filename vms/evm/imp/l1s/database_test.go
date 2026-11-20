// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/types"
)

// TestConfig_UseStandaloneDatabase asserts that a chain already in the node's
// database MUST stay there, and that otherwise the setting MUST be honored.
func TestConfig_UseStandaloneDatabase(t *testing.T) {
	withChain := memdb.New()
	rawdb.WriteCanonicalHash(types.NewChainEthDB(withChain), common.Hash{1}, genesisNumber)

	tests := []struct {
		name    string
		setting bool
		nodeDB  *memdb.Database
		want    bool
	}{
		{
			name:    "true_without_chain",
			setting: true,
			nodeDB:  memdb.New(),
			want:    true,
		},
		{
			name:    "false_without_chain",
			setting: false,
			nodeDB:  memdb.New(),
			want:    false,
		},
		{
			name:    "true_with_chain",
			setting: true,
			nodeDB:  withChain,
			want:    false,
		},
		{
			name:    "false_with_chain",
			setting: false,
			nodeDB:  withChain,
			want:    false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := defaultConfig(t.TempDir())
			c.UseStandaloneDatabase = test.setting
			require.Equal(t, test.want, c.useStandaloneDatabase(logging.NoLog{}, test.nodeDB), "useStandaloneDatabase(...)")
		})
	}
}

// TestStandaloneDatabase asserts that a standalone database MUST hold all
// chain data, so the chain resumes from it after a restart while the node's
// database stays empty.
func TestStandaloneDatabase(t *testing.T) {
	wallet := newWallet(t, 1)
	nodeDB := memdb.New()
	opts := []sutOption{
		withMaxAllocFor(wallet.Addresses()...),
		withDB(nodeDB),
		withChainDataDir(t.TempDir()),
	}

	ctx, sut := newSUT(t, opts...)
	sut.send(ctx, t, wallet.SetNonceAndSign(t, 0, transferTx(common.Address{1}, common.Big1)))
	want := sut.RunConsensusLoop(ctx, t)
	require.NoErrorf(t, sut.Shutdown(ctx), "%T.Shutdown()", sut.VM)

	ctx, restarted := newSUT(t, opts...)
	require.Equal(t, want.ID(), restarted.LastAcceptedID(ctx, t), "LastAcceptedID() after restart")

	count, err := database.Count(nodeDB)
	require.NoError(t, err)
	require.Zero(t, count, "database.Count(node database)")
}
