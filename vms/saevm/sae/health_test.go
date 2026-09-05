// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"testing"
	"time"

	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks/blockstest"
	"github.com/ava-labs/avalanchego/vms/saevm/hook/hookstest"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/ava-labs/avalanchego/vms/saevm/saexec"
)

// TestHealthCheckReportsExecutorDeath is a regression test for the underlying
// [saexec.Executor.processQueue] failure being detectable independently of
// block production: [VM.HealthCheck] MUST report unhealthy as soon as the
// executor's background goroutine has permanently stopped, rather than only
// once a subsequent block happens to be accepted (which may not occur for a
// while, and which previously wasn't even guaranteed to report the failure --
// see [saexec.Executor.TerminalError]).
//
// This deliberately builds only the pieces of a [VM] that [VM.HealthCheck]
// touches, using a [loggingtest.Recorder] (rather than the full [SUT] harness,
// which fails the test on any ERROR/FATAL log and expects such logs to
// originate from the test goroutine, not [saexec.Executor]'s background one).
func TestHealthCheckReportsExecutorDeath(t *testing.T) {
	recorder := loggingtest.NewRecorder(logging.Debug)

	chainDataDir := t.TempDir()
	config := saetest.ChainConfig()
	saedbConfig := saedb.Config{
		CommitInterval:   saedb.DefaultCommitInterval,
		SnapshotCacheMiB: saedb.DefaultSnapshotCacheSizeMiB,
	}
	db := rawdb.NewMemoryDatabase()
	xdb := saetest.NewExecutionResultsDB()
	tdbCfg := saedbConfig.TrieDBConfig(chainDataDir, recorder)

	hooks := hookstest.NewStub(1e6)
	genOpts := []blockstest.GenesisOption{
		blockstest.WithTrieDBConfig(tdbCfg),
		blockstest.WithGasTarget(hooks.Target),
		blockstest.WithBaseFee(1),
	}
	genesis := blockstest.NewGenesis(t, db, config, nil, genOpts...)

	blockOpts := blockstest.WithBlockOptions(
		blockstest.WithLogger(recorder),
		blockstest.WithHooks(hooks),
	)
	chain := blockstest.NewChainBuilder(genesis, blockOpts)
	src := blocks.Source(chain.GetBlock)

	tr, err := saedb.NewTracker(db, saedbConfig, genesis.EthBlock().Root(), chainDataDir, recorder)
	require.NoError(t, err, "saedb.NewTracker()")
	exec, err := saexec.New(genesis, src.AsHeaderSource(), config, db, xdb, tr, hooks, recorder, prometheus.NewRegistry())
	require.NoError(t, err, "saexec.New()")
	t.Cleanup(exec.Close)

	vm := &VM{exec: exec}
	ctx := t.Context()

	if _, err := vm.HealthCheck(ctx); err != nil {
		t.Fatalf("HealthCheck() before any failure: got %v, want nil", err)
	}

	// Build two blocks but only enqueue the second one, orphaning it exactly
	// like the [saexec] package's own regression tests, to kill the executor's
	// background goroutine.
	_ = chain.NewBlock(t, nil)
	orphan := chain.NewBlock(t, nil)
	require.NoError(t, exec.Enqueue(ctx, orphan), "Enqueue() of the block with an unexecuted parent")

	require.Eventuallyf(t, func() bool {
		return exec.TerminalError() != nil
	}, 5*time.Second, 10*time.Millisecond, "%T.TerminalError() after the injected failure", exec)

	_, err = vm.HealthCheck(ctx)
	require.Errorf(t, err, "%T.HealthCheck() after the executor's background goroutine died", vm)
}
