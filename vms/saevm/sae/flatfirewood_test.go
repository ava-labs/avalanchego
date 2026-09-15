// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/saevm/flatfirewood"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest/escrow"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
)

// TestFlatFirewoodServesPrunedHeights runs a node on the flatfirewood scheme,
// pushes a state-changing block far behind the Firewood revision window with
// empty blocks, and checks that the stateful RPCs still answer at that height,
// both before and after a restart whose last committed block left the state
// root unchanged.
func TestFlatFirewoodServesPrunedHeights(t *testing.T) {
	t.Parallel()

	const commitInterval = 2 // RevisionsInMemory = 2 * commitInterval

	var srcDB database.Database
	srcHDB := saetest.NewHeightIndexDB()
	tempDir := t.TempDir()
	withFlatFirewood := options.Func[sutConfig](func(c *sutConfig) {
		c.logLevel = logging.Warn
		c.vmConfig.DBConfig.Scheme = flatfirewood.Scheme
		c.dataDir = tempDir
	})

	sutOpt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))
	ctx, src := newSUT(t, 1, sutOpt, withExecResultsDB(srcHDB), withCommitInterval(commitInterval), withFlatFirewood,
		options.Func[sutConfig](func(c *sutConfig) { srcDB = c.db }),
	)

	escrowAddr := src.deployEscrow(t)
	const depositVal = 42
	recipient := common.Address{'r', 'e', 'c', 'v'}
	deposit := src.depositToEscrow(t, escrowAddr, recipient, big.NewInt(depositVal))
	vmTime.AdvanceToSettle(ctx, t, deposit)

	// Empty blocks leave the state root unchanged and push the deposit block
	// well past the Firewood revision window.
	for range 4 * commitInterval {
		vmTime.Advance(850 * time.Millisecond)
		b := src.runConsensusLoop(t)
		vmTime.AdvanceToSettle(ctx, t, b)
	}

	storageKey := escrow.StorageKeyForBalance(recipient)
	wantStorage := uint256.NewInt(depositVal).PaddedBytes(32)
	checkHistoricalReads := func(t *testing.T, sut *SUT) {
		t.Helper()
		blockNum := deposit.Number()
		got, err := sut.BalanceAt(ctx, escrowAddr, blockNum)
		require.NoError(t, err, "BalanceAt()")
		require.Zero(t, big.NewInt(depositVal).Cmp(got), "BalanceAt(): want %d, got %s", depositVal, got)
		gotStorage, err := sut.StorageAt(ctx, escrowAddr, storageKey, blockNum)
		require.NoError(t, err, "StorageAt()")
		require.Equal(t, wantStorage, gotStorage, "StorageAt()")
		code, err := sut.CodeAt(ctx, escrowAddr, blockNum)
		require.NoError(t, err, "CodeAt()")
		require.Equal(t, escrow.ByteCode(), code, "CodeAt()")

	}
	t.Run("before_restart", func(t *testing.T) { checkHistoricalReads(t, src) })

	src.close()
	newDB := saetest.CopyDB(t, srcDB)
	_, sut := newSUT(t, 1, sutOpt, withExecResultsDB(srcHDB.Clone()), withCommitInterval(commitInterval), withFlatFirewood,
		options.Func[sutConfig](func(c *sutConfig) { c.db = newDB }),
	)
	t.Run("after_restart_with_empty_tail", func(t *testing.T) { checkHistoricalReads(t, sut) })
}
