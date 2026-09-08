// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package paralleltest provides a test harness for [parallel] precompiles
// executing under SAE.
package paralleltest

import (
	"context"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/libevm/precompiles/parallel"
	"github.com/ava-labs/libevm/params"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks/blockstest"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/ava-labs/avalanchego/vms/saevm/saexec"

	saehookstest "github.com/ava-labs/avalanchego/vms/saevm/hook/hookstest"
	libevmhookstest "github.com/ava-labs/libevm/libevm/hookstest"
)

// NewExecutor returns a new SAE block-execution queue with a precompile,
// registered at the provided address, that sources results from the
// [parallel.Handler]. The [saexec.Executor] will have a single, genesis block,
// derived from the provided alloc.
//
// This function registers libevm hooks and is therefore not safe for concurrent
// use across multiple calls, nor with other libevm registrations.
func NewExecutor[CommonData, Prefetch any, R parallel.PrecompileResult, Aggregated any](
	tb testing.TB,
	logger logging.Logger,
	db ethdb.Database,
	config *params.ChainConfig,
	alloc types.GenesisAlloc,
	precompileAddr common.Address,
	handler parallel.Handler[CommonData, Prefetch, R, Aggregated],
	prefetchers, processors int,
) (*saexec.Executor, *blockstest.ChainBuilder) {
	tb.Helper()

	// Although not used until later, the [libevmhookstest.Stub] needs to be
	// registered after the call to [core.SetupGenesisBlock].
	genesis := blockstest.NewGenesis(tb, db, config, alloc)

	par := parallel.New(prefetchers, processors)
	precompile := parallel.AddAsPrecompile(par, handler)

	vm.RegisterHooks(vmHooks{Processor: par})
	tb.Cleanup(vm.TestOnlyClearRegisteredHooks)
	stub := &libevmhookstest.Stub{
		PrecompileOverrides: map[common.Address]libevm.PrecompiledContract{
			precompileAddr: vm.NewStatefulPrecompile(precompile),
		},
	}
	stub.Register(tb)

	hooks := saehookstest.NewStub(100e6)
	hooks.StartExecutingBlockFn = func(r params.Rules, sdb *state.StateDB, _ *types.Header, b *types.Block) error {
		return par.StartBlock(sdb, r, b)
	}
	hooks.FinishExecutingBlockFn = func(sdb *state.StateDB, b *types.Block, rs types.Receipts) error {
		par.FinishBlock(sdb, b, rs)
		return nil
	}

	xdb := saetest.NewExecutionResultsDB()
	chain := blockstest.NewChainBuilder(genesis)
	src := blocks.Source(chain.GetBlock).AsHeaderSource()
	dbConfig := saedb.Config{CommitInterval: 4096}

	tr, err := saedb.NewTracker(db, dbConfig, genesis.Hash(), tb.TempDir(), logger)
	require.NoError(tb, err, "saedb.NewTracker()")

	exec, err := saexec.New(genesis, src, config, db, xdb, tr, hooks, logger, prometheus.NewRegistry())
	require.NoError(tb, err, "saexec.New()")

	tb.Cleanup(func() {
		if !tb.Failed() {
			ctx := context.WithoutCancel(tb.Context())
			assert.NoErrorf(tb, chain.Last().WaitUntilExecuted(ctx), "%T.Last().WaitUntilExecuted()", chain)
		}
		exec.Close()
		assert.NoErrorf(tb, tr.Close(exec.LastExecuted().PostExecutionStateRoot()), "%T.Close()", tr)
		par.Close()
	})

	return exec, chain
}

type vmHooks struct {
	vm.NOOPHooks
	*parallel.Processor
}

func (h vmHooks) PreprocessingGasCharge(tx common.Hash) (uint64, error) {
	return h.Processor.PreprocessingGasCharge(tx)
}
